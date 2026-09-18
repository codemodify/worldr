// Package researchapp presents bounded local datasets as linked table, chart,
// and spatial views inside the native workspace.
package researchapp

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxDatasetBytes = 16 << 20
	maxRows         = 20000
	maxColumns      = 32
	maxCells        = 500000
	maxCellBytes    = 512
)

type Dataset struct {
	Columns []string
	Rows    [][]string
	Values  [][]float64 // column-major; NaN represents an empty or nonnumeric cell.
	Numeric []bool
	Minimum []float64
	Maximum []float64
}

func (d *Dataset) NumericColumns() []int {
	if d == nil {
		return nil
	}
	result := make([]int, 0, len(d.Columns))
	for column, numeric := range d.Numeric {
		if numeric {
			result = append(result, column)
		}
	}
	return result
}

func cleanCell(value string) string {
	value = strings.ToValidUTF8(value, "")
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\t' {
			return ' '
		}
		return r
	}, value)
	if len(value) <= maxCellBytes {
		return value
	}
	n := maxCellBytes
	for n > 0 && !utf8.RuneStart(value[n]) {
		n--
	}
	return value[:n]
}

func uniqueColumns(columns []string) ([]string, error) {
	if len(columns) == 0 || len(columns) > maxColumns {
		return nil, fmt.Errorf("dataset must contain 1..%d columns", maxColumns)
	}
	seen := make(map[string]int, len(columns))
	result := make([]string, len(columns))
	for i, column := range columns {
		column = strings.TrimSpace(cleanCell(column))
		if column == "" {
			column = fmt.Sprintf("column_%d", i+1)
		}
		seen[column]++
		if seen[column] > 1 {
			column = fmt.Sprintf("%s_%d", column, seen[column])
		}
		result[i] = column
	}
	return result, nil
}

func finishDataset(columns []string, rows [][]string) (*Dataset, error) {
	columns, err := uniqueColumns(columns)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("dataset has no observations")
	}
	d := &Dataset{Columns: columns, Rows: rows, Values: make([][]float64, len(columns)), Numeric: make([]bool, len(columns)), Minimum: make([]float64, len(columns)), Maximum: make([]float64, len(columns))}
	for column := range columns {
		d.Values[column] = make([]float64, len(rows))
		d.Numeric[column] = true
		d.Minimum[column], d.Maximum[column] = math.Inf(1), math.Inf(-1)
		seenNumber := false
		for row := range rows {
			cell := ""
			if column < len(rows[row]) {
				cell = strings.TrimSpace(rows[row][column])
			}
			value := math.NaN()
			if cell != "" {
				parsed, parseErr := strconv.ParseFloat(cell, 64)
				if parseErr != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
					d.Numeric[column] = false
				} else {
					value, seenNumber = parsed, true
					d.Minimum[column] = min(d.Minimum[column], parsed)
					d.Maximum[column] = max(d.Maximum[column], parsed)
				}
			}
			d.Values[column][row] = value
		}
		d.Numeric[column] = d.Numeric[column] && seenNumber
		if !d.Numeric[column] {
			d.Minimum[column], d.Maximum[column] = 0, 0
		}
	}
	if len(d.NumericColumns()) == 0 {
		return nil, fmt.Errorf("dataset has no numeric columns")
	}
	return d, nil
}

func parseCSV(data []byte, comma rune) (*Dataset, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma, r.FieldsPerRecord, r.ReuseRecord = comma, -1, true
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read dataset header: %w", err)
	}
	columns := append([]string(nil), header...)
	if len(columns) == 0 || len(columns) > maxColumns {
		return nil, fmt.Errorf("dataset must contain 1..%d columns", maxColumns)
	}
	rows := make([][]string, 0, min(1024, maxRows))
	for len(rows) < maxRows {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read dataset row %d: %w", len(rows)+2, err)
		}
		if len(record) > len(columns) {
			return nil, fmt.Errorf("dataset row %d has %d fields; header has %d", len(rows)+2, len(record), len(columns))
		}
		row := make([]string, len(columns))
		for i := range record {
			row[i] = cleanCell(record[i])
		}
		rows = append(rows, row)
		if len(rows)*len(columns) > maxCells {
			return nil, fmt.Errorf("dataset exceeds the %d-cell limit", maxCells)
		}
	}
	if len(rows) == maxRows {
		if _, err := r.Read(); err != io.EOF {
			return nil, fmt.Errorf("dataset exceeds the %d-row limit", maxRows)
		}
	}
	return finishDataset(columns, rows)
}

func jsonCell(value any) (string, error) {
	switch value := value.(type) {
	case nil:
		return "", nil
	case string:
		return cleanCell(value), nil
	case json.Number:
		if _, err := value.Float64(); err != nil {
			return "", fmt.Errorf("invalid JSON number")
		}
		return value.String(), nil
	case bool:
		return strconv.FormatBool(value), nil
	default:
		return "", fmt.Errorf("nested JSON values are not dataset cells")
	}
}

func parseJSON(data []byte) (*Dataset, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var records []map[string]any
	if err := decoder.Decode(&records); err != nil {
		return nil, fmt.Errorf("read JSON dataset: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("JSON dataset requires one array")
	}
	if len(records) == 0 || len(records) > maxRows {
		return nil, fmt.Errorf("JSON dataset must contain 1..%d objects", maxRows)
	}
	set := make(map[string]bool)
	for row, record := range records {
		normalized := make(map[string]any, len(record))
		for rawKey, value := range record {
			key := cleanCell(strings.TrimSpace(rawKey))
			if key == "" {
				return nil, fmt.Errorf("JSON dataset contains an empty column name")
			}
			if _, duplicate := normalized[key]; duplicate {
				return nil, fmt.Errorf("JSON row %d contains duplicate normalized column %q", row+1, key)
			}
			normalized[key] = value
			set[key] = true
		}
		records[row] = normalized
	}
	if len(set) == 0 || len(set) > maxColumns || len(set)*len(records) > maxCells {
		return nil, fmt.Errorf("JSON dataset exceeds column or cell limits")
	}
	columns := make([]string, 0, len(set))
	for key := range set {
		columns = append(columns, key)
	}
	sort.Strings(columns)
	rows := make([][]string, len(records))
	for i, record := range records {
		row := make([]string, len(columns))
		for column, key := range columns {
			value, err := jsonCell(record[key])
			if err != nil {
				return nil, fmt.Errorf("JSON row %d column %q: %w", i+1, key, err)
			}
			row[column] = value
		}
		rows[i] = row
	}
	return finishDataset(columns, rows)
}

func readDataset(file *os.File, name string) (*Dataset, error) {
	if file == nil {
		return nil, fmt.Errorf("dataset requires an open file")
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxDatasetBytes {
		return nil, fmt.Errorf("dataset must be a regular file between 1 byte and 16 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxDatasetBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDatasetBytes {
		return nil, fmt.Errorf("dataset exceeds the 16 MiB limit")
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".csv":
		return parseCSV(data, ',')
	case ".tsv":
		return parseCSV(data, '\t')
	case ".json":
		return parseJSON(data)
	default:
		return nil, fmt.Errorf("unsupported dataset type")
	}
}
