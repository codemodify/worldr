package native

import "errors"

// ErrOutOfDate asks the display adapter to recreate its swapchain before retry.
var ErrOutOfDate = errors.New("Vulkan swapchain needs resize")

// ErrNotReady means the host currently has no presentable image (for example,
// while the window is minimized). The event loop may skip and retry the frame.
var ErrNotReady = errors.New("Vulkan swapchain image is not ready")
