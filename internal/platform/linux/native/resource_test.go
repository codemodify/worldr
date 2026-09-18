package native

import (
	"errors"
	"testing"
)

func TestNativeResultCategoriesKeepNumericEvidence(t *testing.T) {
	cases := []struct {
		message  string
		category error
		code     int32
	}{
		{"scene command submission failed (VkResult -4)", ErrDeviceLost, -4},
		{"renderer memory budget exceeded: used=1 request=2 limit=2 (VkResult -2)", ErrOutOfMemory, -2},
		{"host allocation failed (VkResult -1)", ErrOutOfMemory, -1},
		{"scene GPU fence wait failed (2s limit) (VkResult 2)", ErrGPUTimeout, 2},
		{"surface capabilities (VkResult -1000000000)", ErrSurfaceLost, -1000000000},
		{"resize (VkResult -1000001004)", ErrOutOfDate, -1000001004},
		{"surface has zero extent (VkResult 1)", ErrNotReady, 1},
	}
	for _, test := range cases {
		err := nativeError(test.message)
		var result *VulkanError
		if !errors.Is(err, test.category) || !errors.As(err, &result) || result.Code != test.code || err.Error() != test.message {
			t.Fatalf("lost typed result for %q: %#v", test.message, err)
		}
	}
	if !errors.Is(nativeError("Vulkan target requires recovery"), ErrNeedsRecovery) {
		t.Fatal("incomplete target state was not recoverable")
	}
	if errors.Is(nativeError("unrelated error"), ErrDeviceLost) {
		t.Fatal("unrelated error became a device reset")
	}
}
