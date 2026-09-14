// Package linux is the Linux platform boundary for DRM/KMS and Vulkan.
//
// cgo/FFI lives in package native (libvulkan + libdrm). First bring-up
// target: Intel Arrow Lake iGPU on machine "abox" (Mesa 26.2.2, Vulkan 1.4).
package linux

// BringUpTarget names the first hardware the real present path should boot on.
const BringUpTarget = "abox: Intel Arrow Lake iGPU, Mesa 26.2.2, Vulkan 1.4"
