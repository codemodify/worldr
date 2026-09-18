package native

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	ErrNeedsRecovery = errors.New("Vulkan renderer requires recovery")
	ErrDeviceLost    = errors.New("Vulkan device lost")
	ErrOutOfMemory   = errors.New("Vulkan memory allocation or budget exhausted")
	ErrSurfaceLost   = errors.New("Vulkan display surface lost")
	ErrGPUTimeout    = errors.New("Vulkan GPU wait timed out")
)

const DefaultMemoryBudget uint64 = 1 << 30

// MemoryStats counts actual VkDeviceMemory allocation requirements, including
// staging, retained assets, targets and effects. Swapchain/driver allocations and
// Go CPU copies are outside this accounting. Calls share the render goroutine.
type MemoryStats struct {
	AllocatedBytes, PeakBytes, BudgetBytes uint64
	Images, Buffers                        uint32
}

// VulkanError retains the operation text and numeric VkResult while supporting
// errors.Is for failures the host can handle without parsing driver messages.
type VulkanError struct {
	Code    int32
	Message string
}

func (e *VulkanError) Error() string { return e.Message }
func (e *VulkanError) Unwrap() error {
	switch e.Code {
	case -4:
		return ErrDeviceLost
	case -1, -2:
		return ErrOutOfMemory
	case -1000000000:
		return ErrSurfaceLost
	case -1000001004:
		return ErrOutOfDate
	case 1:
		return ErrNotReady
	case 2:
		return ErrGPUTimeout
	}
	return nil
}

func nativeError(message string) error {
	if message == "Vulkan target requires recovery" || message == "scene session failed; reopen Vulkan session" {
		return fmt.Errorf("%s: %w", message, ErrNeedsRecovery)
	}
	// seterr's result suffix is a fixed part of the private C/Go ABI.
	marker := strings.LastIndex(message, " (VkResult ")
	if marker >= 0 && strings.HasSuffix(message, ")") {
		if code, err := strconv.ParseInt(message[marker+11:len(message)-1], 10, 32); err == nil {
			return &VulkanError{int32(code), message}
		}
	}
	return errors.New(message)
}
