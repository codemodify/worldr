// Package linux is the Linux platform boundary for DRM/KMS and Vulkan.
//
// Phase 0: notes only. No DRM, KMS, or Vulkan bindings in this tree yet.
// cgo/FFI is permitted here later, and only here (plus the Vulkan ABI), not
// as a second compositor codebase in C/C++/Rust.
//
// First bring-up target: Intel Arrow Lake iGPU on machine "abox"
// (Mesa 26.2.2, Vulkan 1.4). NVIDIA and AMD stay in scope after Intel.
package linux

// BringUpTarget names the first hardware the real present path should boot on.
const BringUpTarget = "abox: Intel Arrow Lake iGPU, Mesa 26.2.2, Vulkan 1.4"
