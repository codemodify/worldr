package researchapp

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCSVBuildsBoundedNumericColumns(t *testing.T) {
	dataset, err := parseCSV([]byte("time,signal,label\n0,2.5,alpha\n1,4.5,beta\n2,,gamma\n"), ',')
	if err != nil {
		t.Fatal(err)
	}
	if len(dataset.Rows) != 3 || len(dataset.Columns) != 3 || !dataset.Numeric[0] || !dataset.Numeric[1] || dataset.Numeric[2] {
		t.Fatalf("unexpected dataset: %+v", dataset)
	}
	if dataset.Minimum[1] != 2.5 || dataset.Maximum[1] != 4.5 || !math.IsNaN(dataset.Values[1][2]) {
		t.Fatalf("numeric range or missing value was lost: %+v", dataset.Values[1])
	}
	if _, err := parseCSV([]byte("a,b\n1,2,3\n"), ','); err == nil {
		t.Fatal("accepted a row wider than its header")
	}
	if _, err := parseCSV([]byte("name\nalpha\n"), ','); err == nil {
		t.Fatal("accepted a dataset without numeric dimensions")
	}
}

func TestParseJSONNormalizesKeysDeterministically(t *testing.T) {
	dataset, err := parseJSON([]byte(`[{" y ":2,"x":1,"note":"a"},{"x":3," y ":4,"note":"b"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(dataset.Columns, ",") != "note,x,y" || dataset.Rows[1][2] != "4" {
		t.Fatalf("normalized JSON columns lost data: %#v %#v", dataset.Columns, dataset.Rows)
	}
	if _, err := parseJSON([]byte(`[{"x":1,"nested":{"y":2}}]`)); err == nil {
		t.Fatal("accepted a nested JSON cell")
	}
	if _, err := parseJSON([]byte(`[{"x":1," x ":2}]`)); err == nil {
		t.Fatal("accepted duplicate normalized columns")
	}
}

func TestReadDatasetUsesDescriptorAndRejectsUnboundedInput(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "signals.tsv")
	if err := os.WriteFile(path, []byte("x\ty\n1\t2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := readDataset(file, path)
	file.Close()
	if err != nil || len(dataset.Rows) != 1 {
		t.Fatal(dataset, err)
	}
	large := filepath.Join(directory, "large.csv")
	file, err = os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(maxDatasetBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	file, err = os.Open(large)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err = readDataset(file, large); err == nil {
		t.Fatal("accepted an oversized dataset")
	}
}

func TestDatasetPathKeepsOrdinaryJSONInTextPreview(t *testing.T) {
	for _, path := range []string{"run.CSV", "signals.tsv", "experiment.worldr-data.json"} {
		if !IsDatasetPath(path) {
			t.Fatal("dataset extension not recognized", path)
		}
	}
	for _, path := range []string{"settings.json", "table.txt", "fake.csv.exe"} {
		if IsDatasetPath(path) {
			t.Fatal("ordinary file claimed as dataset", path)
		}
	}
}
