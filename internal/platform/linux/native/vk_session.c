#define VK_USE_PLATFORM_DISPLAY_KHR
#include "vk_session.h"

#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <unistd.h>
#include <vulkan/vulkan.h>
#include <drm_fourcc.h>

#define WORLDR_MAX_IMAGES 8
#define WORLDR_DMABUF_SLOTS 32
/* Finite GPU wait so a lost DRM master / hung KMS does not wedge the TTY. */
#define WORLDR_VK_WAIT_NS 2000000000ull

typedef struct {
	int used;
	VkImage image;
	VkDeviceMemory mem[4];
	int nmem;
	uint32_t w, h;
	VkFormat fmt;
} worldr_dma_slot;

struct worldr_vk {
	int mode;
	VkInstance instance;
	VkPhysicalDevice phys;
	VkDevice device;
	VkQueue queue;
	uint32_t queue_family;
	VkSurfaceKHR surface;
	VkSwapchainKHR swapchain;
	VkImage images[WORLDR_MAX_IMAGES];
	uint32_t image_count;
	VkCommandPool cmd_pool;
	VkCommandBuffer cmd;
	VkSemaphore img_avail;
	VkSemaphore done;
	VkFence fence;
	VkBuffer staging;
	VkDeviceMemory staging_mem;
	void *staging_map;
	VkDeviceSize staging_size;
	VkImage headless_image;
	VkDeviceMemory headless_mem;
	void *headless_map;
	uint32_t width;
	uint32_t height;
	uint32_t vendor_id;
	char device_name[256];
	VkFormat format;
	int dmabuf_ok;
	int timeline_ok;
	int ext_sem_fd;
	uint32_t display_planes;
	VkDisplayKHR acquired;
	worldr_dma_slot dma[WORLDR_DMABUF_SLOTS];
};

#ifndef VK_EXT_ACQUIRE_DRM_DISPLAY_EXTENSION_NAME
#define VK_EXT_ACQUIRE_DRM_DISPLAY_EXTENSION_NAME "VK_EXT_acquire_drm_display"
#endif

typedef VkResult(VKAPI_PTR *worldr_vk_get_drm_display_fn)(VkPhysicalDevice physicalDevice, int drmFd, uint32_t connectorId, VkDisplayKHR *display);
typedef VkResult(VKAPI_PTR *worldr_vk_acquire_drm_display_fn)(VkPhysicalDevice physicalDevice, int drmFd, VkDisplayKHR display);

static void seterr(char *err, int errlen, const char *fmt, VkResult r)
{
	if (!err || errlen <= 0) {
		return;
	}
	if (r == VK_SUCCESS) {
		snprintf(err, (size_t)errlen, "%s", fmt);
		return;
	}
	snprintf(err, (size_t)errlen, "%s (VkResult %d)", fmt, (int)r);
}

static uint32_t find_memory(VkPhysicalDevice phys, uint32_t type_bits, VkMemoryPropertyFlags flags)
{
	VkPhysicalDeviceMemoryProperties mp;
	vkGetPhysicalDeviceMemoryProperties(phys, &mp);
	for (uint32_t i = 0; i < mp.memoryTypeCount; i++) {
		if ((type_bits & (1u << i)) && (mp.memoryTypes[i].propertyFlags & flags) == flags) {
			return i;
		}
	}
	return UINT32_MAX;
}

static int prefer_score(const VkPhysicalDeviceProperties *p)
{
	int score = 0;
	if (p->vendorID == 0x8086) {
		score += 100; /* Intel first (abox) */
	} else if (p->vendorID == 0x1002) {
		score += 50;
	} else if (p->vendorID == 0x10de) {
		score += 40;
	}
	if (p->deviceType == VK_PHYSICAL_DEVICE_TYPE_INTEGRATED_GPU) {
		score += 10;
	} else if (p->deviceType == VK_PHYSICAL_DEVICE_TYPE_DISCRETE_GPU) {
		score += 8;
	}
	return score;
}

static int create_instance(int mode, int acquire_drm, VkInstance *out, char *err, int errlen)
{
	const char *exts_display[] = {
		VK_KHR_SURFACE_EXTENSION_NAME,
		VK_KHR_DISPLAY_EXTENSION_NAME,
	};
	const char *exts_acquire[] = {
		VK_KHR_SURFACE_EXTENSION_NAME,
		VK_KHR_DISPLAY_EXTENSION_NAME,
		VK_EXT_ACQUIRE_DRM_DISPLAY_EXTENSION_NAME,
	};
	VkApplicationInfo app = {0};
	app.sType = VK_STRUCTURE_TYPE_APPLICATION_INFO;
	app.pApplicationName = "worldr-shell";
	app.applicationVersion = VK_MAKE_VERSION(0, 1, 0);
	app.pEngineName = "worldr";
	app.engineVersion = VK_MAKE_VERSION(0, 1, 0);
	app.apiVersion = VK_API_VERSION_1_2;

	VkInstanceCreateInfo ci = {0};
	ci.sType = VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO;
	ci.pApplicationInfo = &app;
	if (mode == WORLDR_VK_DISPLAY) {
		if (acquire_drm) {
			ci.enabledExtensionCount = 3;
			ci.ppEnabledExtensionNames = exts_acquire;
		} else {
			ci.enabledExtensionCount = 2;
			ci.ppEnabledExtensionNames = exts_display;
		}
	}

	VkResult r = vkCreateInstance(&ci, NULL, out);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkCreateInstance failed — is libvulkan.so.1 / an ICD installed?", r);
		return -1;
	}
	return 0;
}

static int pick_phys(VkInstance inst, int mode, VkPhysicalDevice *out, uint32_t *qfamily, char *err, int errlen)
{
	uint32_t n = 0;
	vkEnumeratePhysicalDevices(inst, &n, NULL);
	if (n == 0) {
		seterr(err, errlen, "no Vulkan physical devices (no GPU ICD, or no /dev/dri)", VK_SUCCESS);
		return -1;
	}
	VkPhysicalDevice *devs = (VkPhysicalDevice *)calloc(n, sizeof(*devs));
	if (!devs) {
		seterr(err, errlen, "oom", VK_SUCCESS);
		return -1;
	}
	vkEnumeratePhysicalDevices(inst, &n, devs);

	int best = -1, best_score = -1;
	uint32_t best_q = 0;
	for (uint32_t i = 0; i < n; i++) {
		VkPhysicalDeviceProperties props;
		vkGetPhysicalDeviceProperties(devs[i], &props);
		uint32_t qn = 0;
		vkGetPhysicalDeviceQueueFamilyProperties(devs[i], &qn, NULL);
		VkQueueFamilyProperties *qs = (VkQueueFamilyProperties *)calloc(qn, sizeof(*qs));
		if (!qs) {
			continue;
		}
		vkGetPhysicalDeviceQueueFamilyProperties(devs[i], &qn, qs);
		int q = -1;
		for (uint32_t f = 0; f < qn; f++) {
			if (qs[f].queueFlags & VK_QUEUE_GRAPHICS_BIT) {
				q = (int)f;
				break;
			}
		}
		free(qs);
		if (q < 0) {
			continue;
		}
		int score = prefer_score(&props);
		if (score > best_score) {
			best_score = score;
			best = (int)i;
			best_q = (uint32_t)q;
		}
	}
	if (best < 0) {
		free(devs);
		seterr(err, errlen, "no graphics-capable Vulkan device", VK_SUCCESS);
		return -1;
	}
	*out = devs[best];
	*qfamily = best_q;
	(void)mode;
	free(devs);
	return 0;
}

static int has_dev_ext(VkPhysicalDevice phys, const char *name)
{
	uint32_t n = 0;
	vkEnumerateDeviceExtensionProperties(phys, NULL, &n, NULL);
	if (!n) {
		return 0;
	}
	VkExtensionProperties *p = (VkExtensionProperties *)calloc(n, sizeof(*p));
	if (!p) {
		return 0;
	}
	vkEnumerateDeviceExtensionProperties(phys, NULL, &n, p);
	int ok = 0;
	for (uint32_t i = 0; i < n; i++) {
		if (strcmp(p[i].extensionName, name) == 0) {
			ok = 1;
			break;
		}
	}
	free(p);
	return ok;
}

static int create_device(worldr_vk *vk, int need_swapchain, char *err, int errlen)
{
	float prio = 1.0f;
	VkDeviceQueueCreateInfo qci = {0};
	qci.sType = VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO;
	qci.queueFamilyIndex = vk->queue_family;
	qci.queueCount = 1;
	qci.pQueuePriorities = &prio;

	const char *want[] = {
		VK_KHR_SWAPCHAIN_EXTENSION_NAME,
		VK_KHR_EXTERNAL_MEMORY_FD_EXTENSION_NAME,
		VK_EXT_EXTERNAL_MEMORY_DMA_BUF_EXTENSION_NAME,
		VK_EXT_IMAGE_DRM_FORMAT_MODIFIER_EXTENSION_NAME,
		VK_KHR_IMAGE_FORMAT_LIST_EXTENSION_NAME,
		VK_EXT_QUEUE_FAMILY_FOREIGN_EXTENSION_NAME,
		VK_KHR_EXTERNAL_SEMAPHORE_FD_EXTENSION_NAME,
	};
	const char *have[10];
	uint32_t nh = 0;
	for (uint32_t i = 0; i < sizeof(want) / sizeof(want[0]); i++) {
		if (strcmp(want[i], VK_KHR_SWAPCHAIN_EXTENSION_NAME) == 0 && !need_swapchain) {
			continue;
		}
		if (has_dev_ext(vk->phys, want[i])) {
			have[nh++] = want[i];
		}
	}
	vk->dmabuf_ok = has_dev_ext(vk->phys, VK_KHR_EXTERNAL_MEMORY_FD_EXTENSION_NAME) &&
			has_dev_ext(vk->phys, VK_EXT_EXTERNAL_MEMORY_DMA_BUF_EXTENSION_NAME);
	vk->ext_sem_fd = has_dev_ext(vk->phys, VK_KHR_EXTERNAL_SEMAPHORE_FD_EXTENSION_NAME);

	VkPhysicalDeviceTimelineSemaphoreFeatures ts = {0};
	ts.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_TIMELINE_SEMAPHORE_FEATURES;
	VkPhysicalDeviceFeatures2 f2 = {0};
	f2.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_FEATURES_2;
	f2.pNext = &ts;
	vkGetPhysicalDeviceFeatures2(vk->phys, &f2);
	vk->timeline_ok = ts.timelineSemaphore && vk->ext_sem_fd;
	ts.timelineSemaphore = VK_TRUE;

	VkDeviceCreateInfo dci = {0};
	dci.sType = VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO;
	dci.queueCreateInfoCount = 1;
	dci.pQueueCreateInfos = &qci;
	dci.enabledExtensionCount = nh;
	dci.ppEnabledExtensionNames = nh ? have : NULL;
	if (vk->timeline_ok) {
		dci.pNext = &ts;
	}

	VkResult r = vkCreateDevice(vk->phys, &dci, NULL, &vk->device);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkCreateDevice failed", r);
		return -1;
	}
	vkGetDeviceQueue(vk->device, vk->queue_family, 0, &vk->queue);
	return 0;
}

static int create_display_surface(worldr_vk *vk, uint32_t prefer_w, uint32_t prefer_h, char *err, int errlen)
{
	uint32_t ndisp = 0;
	VkResult r = vkGetPhysicalDeviceDisplayPropertiesKHR(vk->phys, &ndisp, NULL);
	if (r != VK_SUCCESS || ndisp == 0) {
		seterr(err, errlen, "VK_KHR_display: no displays. Spare VT as DRM master: Ctrl+Alt+F3 + scripts/try-tty.sh", r);
		return -1;
	}
	VkDisplayPropertiesKHR *disps = (VkDisplayPropertiesKHR *)calloc(ndisp, sizeof(*disps));
	if (!disps) {
		seterr(err, errlen, "oom", VK_SUCCESS);
		return -1;
	}
	vkGetPhysicalDeviceDisplayPropertiesKHR(vk->phys, &ndisp, disps);

	uint32_t nplanes = 0;
	vkGetPhysicalDeviceDisplayPlanePropertiesKHR(vk->phys, &nplanes, NULL);
	vk->display_planes = nplanes;
	VkDisplayPlanePropertiesKHR *planes = NULL;
	if (nplanes) {
		planes = (VkDisplayPlanePropertiesKHR *)calloc(nplanes, sizeof(*planes));
		if (planes) {
			vkGetPhysicalDeviceDisplayPlanePropertiesKHR(vk->phys, &nplanes, planes);
		}
	}

	/* Prefer a display that still has a free plane (currentDisplay == NULL).
	   On a spare VT this is the usual case; on a live Plasma session every
	   plane is held — we still fall back to display 0 so --take-over-display works.
	   When VK_EXT_acquire_drm_display bound a connector, use that display. */
	VkDisplayKHR display = vk->acquired ? vk->acquired : disps[0].display;
	uint32_t plane = 0;
	int picked_free = 0;
	for (uint32_t i = 0; i < nplanes && planes; i++) {
		if (planes[i].currentDisplay != VK_NULL_HANDLE) {
			continue;
		}
		uint32_t nsup = 0;
		vkGetDisplayPlaneSupportedDisplaysKHR(vk->phys, i, &nsup, NULL);
		if (!nsup) {
			continue;
		}
		VkDisplayKHR *sup = (VkDisplayKHR *)calloc(nsup, sizeof(*sup));
		if (!sup) {
			continue;
		}
		vkGetDisplayPlaneSupportedDisplaysKHR(vk->phys, i, &nsup, sup);
		for (uint32_t s = 0; s < nsup && !picked_free; s++) {
			for (uint32_t d = 0; d < ndisp; d++) {
				if (sup[s] == disps[d].display) {
					display = disps[d].display;
					plane = i;
					picked_free = 1;
					break;
				}
			}
		}
		free(sup);
		if (picked_free) {
			break;
		}
	}
	if (!picked_free) {
		for (uint32_t i = 0; i < nplanes && planes; i++) {
			uint32_t nsup = 0;
			vkGetDisplayPlaneSupportedDisplaysKHR(vk->phys, i, &nsup, NULL);
			if (!nsup) {
				continue;
			}
			VkDisplayKHR *sup = (VkDisplayKHR *)calloc(nsup, sizeof(*sup));
			if (!sup) {
				continue;
			}
			vkGetDisplayPlaneSupportedDisplaysKHR(vk->phys, i, &nsup, sup);
			int ok = 0;
			for (uint32_t s = 0; s < nsup; s++) {
				if (sup[s] == display) {
					ok = 1;
					break;
				}
			}
			free(sup);
			if (ok) {
				plane = i;
				break;
			}
		}
	}

	uint32_t nmodes = 0;
	vkGetDisplayModePropertiesKHR(vk->phys, display, &nmodes, NULL);
	if (nmodes == 0) {
		free(planes);
		free(disps);
		seterr(err, errlen, "display has no modes", VK_SUCCESS);
		return -1;
	}
	VkDisplayModePropertiesKHR *modes = (VkDisplayModePropertiesKHR *)calloc(nmodes, sizeof(*modes));
	if (!modes) {
		free(planes);
		free(disps);
		seterr(err, errlen, "oom", VK_SUCCESS);
		return -1;
	}
	vkGetDisplayModePropertiesKHR(vk->phys, display, &nmodes, modes);

	uint32_t mode_i = 0;
	if (prefer_w && prefer_h) {
		for (uint32_t i = 0; i < nmodes; i++) {
			if (modes[i].parameters.visibleRegion.width == prefer_w &&
			    modes[i].parameters.visibleRegion.height == prefer_h) {
				mode_i = i;
				break;
			}
		}
	}

	VkDisplaySurfaceCreateInfoKHR sci = {0};
	sci.sType = VK_STRUCTURE_TYPE_DISPLAY_SURFACE_CREATE_INFO_KHR;
	sci.displayMode = modes[mode_i].displayMode;
	sci.planeIndex = plane;
	sci.planeStackIndex = planes ? planes[plane].currentStackIndex : 0;
	sci.transform = VK_SURFACE_TRANSFORM_IDENTITY_BIT_KHR;
	sci.globalAlpha = 1.0f;
	sci.alphaMode = VK_DISPLAY_PLANE_ALPHA_OPAQUE_BIT_KHR;
	sci.imageExtent = modes[mode_i].parameters.visibleRegion;

	r = vkCreateDisplayPlaneSurfaceKHR(vk->instance, &sci, NULL, &vk->surface);
	vk->width = modes[mode_i].parameters.visibleRegion.width;
	vk->height = modes[mode_i].parameters.visibleRegion.height;
	free(planes);
	free(modes);
	free(disps);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkCreateDisplayPlaneSurfaceKHR failed — not DRM master? Spare VT: Ctrl+Alt+F3 + scripts/try-tty.sh", r);
		return -1;
	}
	return 0;
}

static int create_swapchain(worldr_vk *vk, char *err, int errlen)
{
	VkSurfaceCapabilitiesKHR caps;
	VkResult r = vkGetPhysicalDeviceSurfaceCapabilitiesKHR(vk->phys, vk->surface, &caps);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkGetPhysicalDeviceSurfaceCapabilitiesKHR failed", r);
		return -1;
	}
	if (caps.currentExtent.width != 0xFFFFFFFFu) {
		vk->width = caps.currentExtent.width;
		vk->height = caps.currentExtent.height;
	}

	uint32_t nf = 0;
	vkGetPhysicalDeviceSurfaceFormatsKHR(vk->phys, vk->surface, &nf, NULL);
	VkSurfaceFormatKHR *fmts = (VkSurfaceFormatKHR *)calloc(nf ? nf : 1, sizeof(*fmts));
	if (!fmts) {
		seterr(err, errlen, "oom", VK_SUCCESS);
		return -1;
	}
	if (nf) {
		vkGetPhysicalDeviceSurfaceFormatsKHR(vk->phys, vk->surface, &nf, fmts);
	}
	vk->format = VK_FORMAT_B8G8R8A8_UNORM;
	for (uint32_t i = 0; i < nf; i++) {
		if (fmts[i].format == VK_FORMAT_B8G8R8A8_UNORM || fmts[i].format == VK_FORMAT_B8G8R8A8_SRGB) {
			vk->format = fmts[i].format;
			break;
		}
	}
	free(fmts);

	uint32_t count = caps.minImageCount;
	if (count < 2) {
		count = 2;
	}
	if (caps.maxImageCount && count > caps.maxImageCount) {
		count = caps.maxImageCount;
	}

	VkSwapchainCreateInfoKHR ci = {0};
	ci.sType = VK_STRUCTURE_TYPE_SWAPCHAIN_CREATE_INFO_KHR;
	ci.surface = vk->surface;
	ci.minImageCount = count;
	ci.imageFormat = vk->format;
	ci.imageColorSpace = VK_COLOR_SPACE_SRGB_NONLINEAR_KHR;
	ci.imageExtent.width = vk->width;
	ci.imageExtent.height = vk->height;
	ci.imageArrayLayers = 1;
	ci.imageUsage = VK_IMAGE_USAGE_TRANSFER_DST_BIT | VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT;
	ci.imageSharingMode = VK_SHARING_MODE_EXCLUSIVE;
	ci.preTransform = caps.currentTransform ? caps.currentTransform : VK_SURFACE_TRANSFORM_IDENTITY_BIT_KHR;
	ci.compositeAlpha = VK_COMPOSITE_ALPHA_OPAQUE_BIT_KHR;
	ci.presentMode = VK_PRESENT_MODE_FIFO_KHR;
	ci.clipped = VK_TRUE;

	r = vkCreateSwapchainKHR(vk->device, &ci, NULL, &vk->swapchain);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkCreateSwapchainKHR failed", r);
		return -1;
	}
	vkGetSwapchainImagesKHR(vk->device, vk->swapchain, &vk->image_count, NULL);
	if (vk->image_count > WORLDR_MAX_IMAGES) {
		vk->image_count = WORLDR_MAX_IMAGES;
	}
	r = vkGetSwapchainImagesKHR(vk->device, vk->swapchain, &vk->image_count, vk->images);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkGetSwapchainImagesKHR failed", r);
		return -1;
	}
	return 0;
}

static int create_cmds(worldr_vk *vk, char *err, int errlen)
{
	VkCommandPoolCreateInfo pci = {0};
	pci.sType = VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO;
	pci.flags = VK_COMMAND_POOL_CREATE_RESET_COMMAND_BUFFER_BIT;
	pci.queueFamilyIndex = vk->queue_family;
	VkResult r = vkCreateCommandPool(vk->device, &pci, NULL, &vk->cmd_pool);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkCreateCommandPool failed", r);
		return -1;
	}
	VkCommandBufferAllocateInfo ai = {0};
	ai.sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO;
	ai.commandPool = vk->cmd_pool;
	ai.level = VK_COMMAND_BUFFER_LEVEL_PRIMARY;
	ai.commandBufferCount = 1;
	r = vkAllocateCommandBuffers(vk->device, &ai, &vk->cmd);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkAllocateCommandBuffers failed", r);
		return -1;
	}
	if (vk->mode == WORLDR_VK_DISPLAY) {
		VkSemaphoreCreateInfo si = {.sType = VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO};
		VkFenceCreateInfo fi = {.sType = VK_STRUCTURE_TYPE_FENCE_CREATE_INFO, .flags = VK_FENCE_CREATE_SIGNALED_BIT};
		if (vkCreateSemaphore(vk->device, &si, NULL, &vk->img_avail) != VK_SUCCESS ||
		    vkCreateSemaphore(vk->device, &si, NULL, &vk->done) != VK_SUCCESS ||
		    vkCreateFence(vk->device, &fi, NULL, &vk->fence) != VK_SUCCESS) {
			seterr(err, errlen, "sync object create failed", VK_ERROR_UNKNOWN);
			return -1;
		}
	}
	return 0;
}

static int ensure_staging(worldr_vk *vk, VkDeviceSize size, char *err, int errlen)
{
	if (vk->staging && vk->staging_size >= size) {
		return 0;
	}
	if (vk->staging_map) {
		vkUnmapMemory(vk->device, vk->staging_mem);
		vk->staging_map = NULL;
	}
	if (vk->staging) {
		vkDestroyBuffer(vk->device, vk->staging, NULL);
		vk->staging = VK_NULL_HANDLE;
	}
	if (vk->staging_mem) {
		vkFreeMemory(vk->device, vk->staging_mem, NULL);
		vk->staging_mem = VK_NULL_HANDLE;
	}
	VkBufferCreateInfo bi = {0};
	bi.sType = VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO;
	bi.size = size;
	bi.usage = VK_BUFFER_USAGE_TRANSFER_SRC_BIT;
	bi.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
	VkResult r = vkCreateBuffer(vk->device, &bi, NULL, &vk->staging);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkCreateBuffer staging failed", r);
		return -1;
	}
	VkMemoryRequirements mr;
	vkGetBufferMemoryRequirements(vk->device, vk->staging, &mr);
	uint32_t mi = find_memory(vk->phys, mr.memoryTypeBits, VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT);
	if (mi == UINT32_MAX) {
		seterr(err, errlen, "no host-visible memory for staging", VK_SUCCESS);
		return -1;
	}
	VkMemoryAllocateInfo ai = {0};
	ai.sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO;
	ai.allocationSize = mr.size;
	ai.memoryTypeIndex = mi;
	r = vkAllocateMemory(vk->device, &ai, NULL, &vk->staging_mem);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkAllocateMemory staging failed", r);
		return -1;
	}
	vkBindBufferMemory(vk->device, vk->staging, vk->staging_mem, 0);
	r = vkMapMemory(vk->device, vk->staging_mem, 0, mr.size, 0, &vk->staging_map);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkMapMemory staging failed", r);
		return -1;
	}
	vk->staging_size = size;
	return 0;
}

static int create_headless_image(worldr_vk *vk, char *err, int errlen)
{
	if (!vk->width) {
		vk->width = 64;
	}
	if (!vk->height) {
		vk->height = 64;
	}
	vk->format = VK_FORMAT_B8G8R8A8_UNORM;
	VkImageCreateInfo ii = {0};
	ii.sType = VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO;
	ii.imageType = VK_IMAGE_TYPE_2D;
	ii.format = vk->format;
	ii.extent.width = vk->width;
	ii.extent.height = vk->height;
	ii.extent.depth = 1;
	ii.mipLevels = 1;
	ii.arrayLayers = 1;
	ii.samples = VK_SAMPLE_COUNT_1_BIT;
	ii.tiling = VK_IMAGE_TILING_LINEAR;
	ii.usage = VK_IMAGE_USAGE_TRANSFER_DST_BIT | VK_IMAGE_USAGE_TRANSFER_SRC_BIT;
	ii.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
	ii.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
	VkResult r = vkCreateImage(vk->device, &ii, NULL, &vk->headless_image);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkCreateImage headless failed", r);
		return -1;
	}
	VkMemoryRequirements mr;
	vkGetImageMemoryRequirements(vk->device, vk->headless_image, &mr);
	uint32_t mi = find_memory(vk->phys, mr.memoryTypeBits, VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT);
	if (mi == UINT32_MAX) {
		mi = find_memory(vk->phys, mr.memoryTypeBits, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
	}
	if (mi == UINT32_MAX) {
		seterr(err, errlen, "no memory type for headless image", VK_SUCCESS);
		return -1;
	}
	VkMemoryAllocateInfo ai = {0};
	ai.sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO;
	ai.allocationSize = mr.size;
	ai.memoryTypeIndex = mi;
	r = vkAllocateMemory(vk->device, &ai, NULL, &vk->headless_mem);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkAllocateMemory headless failed", r);
		return -1;
	}
	vkBindImageMemory(vk->device, vk->headless_image, vk->headless_mem, 0);
	return 0;
}

static void barrier(VkCommandBuffer cmd, VkImage image, VkImageLayout from, VkImageLayout to, VkAccessFlags src, VkAccessFlags dst, VkPipelineStageFlags srcs, VkPipelineStageFlags dsts)
{
	VkImageMemoryBarrier b = {0};
	b.sType = VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER;
	b.srcAccessMask = src;
	b.dstAccessMask = dst;
	b.oldLayout = from;
	b.newLayout = to;
	b.srcQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED;
	b.dstQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED;
	b.image = image;
	b.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
	b.subresourceRange.levelCount = 1;
	b.subresourceRange.layerCount = 1;
	vkCmdPipelineBarrier(cmd, srcs, dsts, 0, 0, NULL, 0, NULL, 1, &b);
}

static int submit_wait(worldr_vk *vk, VkSemaphore wait, VkPipelineStageFlags wait_stage, VkSemaphore signal, char *err, int errlen)
{
	VkSubmitInfo si = {0};
	si.sType = VK_STRUCTURE_TYPE_SUBMIT_INFO;
	si.commandBufferCount = 1;
	si.pCommandBuffers = &vk->cmd;
	if (wait) {
		si.waitSemaphoreCount = 1;
		si.pWaitSemaphores = &wait;
		si.pWaitDstStageMask = &wait_stage;
	}
	if (signal) {
		si.signalSemaphoreCount = 1;
		si.pSignalSemaphores = &signal;
	}
	VkResult r = vkQueueSubmit(vk->queue, 1, &si, vk->fence ? vk->fence : VK_NULL_HANDLE);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkQueueSubmit failed", r);
		return -1;
	}
	if (vk->fence) {
		r = vkWaitForFences(vk->device, 1, &vk->fence, VK_TRUE, WORLDR_VK_WAIT_NS);
		if (r == VK_TIMEOUT) {
			seterr(err, errlen, "GPU wait timed out (2s) — lost DRM master or hung KMS. Spare TTY: Ctrl+Alt+F4, pkill worldr-shell, Ctrl+Alt+F1", r);
			return -1;
		}
		if (r != VK_SUCCESS) {
			seterr(err, errlen, "vkWaitForFences failed", r);
			return -1;
		}
	} else {
		vkQueueWaitIdle(vk->queue);
	}
	return 0;
}

static int acquire_drm_display(worldr_vk *vk, int drm_fd, uint32_t connector_id, char *err, int errlen)
{
	if (!vk || !vk->instance || drm_fd < 0 || connector_id == 0) {
		seterr(err, errlen, "VK_EXT_acquire_drm_display: missing fd or connector", VK_SUCCESS);
		return -1;
	}
	worldr_vk_get_drm_display_fn get_disp = (worldr_vk_get_drm_display_fn)vkGetInstanceProcAddr(vk->instance, "vkGetDrmDisplayEXT");
	worldr_vk_acquire_drm_display_fn acq = (worldr_vk_acquire_drm_display_fn)vkGetInstanceProcAddr(vk->instance, "vkAcquireDrmDisplayEXT");
	if (!get_disp || !acq) {
		seterr(err, errlen, "VK_EXT_acquire_drm_display entry points missing", VK_SUCCESS);
		return -1;
	}
	VkDisplayKHR disp = VK_NULL_HANDLE;
	VkResult r = get_disp(vk->phys, drm_fd, connector_id, &disp);
	if (r != VK_SUCCESS || disp == VK_NULL_HANDLE) {
		seterr(err, errlen, "vkGetDrmDisplayEXT failed", r);
		return -1;
	}
	r = acq(vk->phys, drm_fd, disp);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkAcquireDrmDisplayEXT failed — not DRM master?", r);
		return -1;
	}
	vk->acquired = disp;
	return 0;
}

static int vk_create_ex(int mode, uint32_t prefer_w, uint32_t prefer_h, int drm_fd, uint32_t connector_id,
			worldr_vk **out, char *err, int errlen)
{
	worldr_vk *vk = (worldr_vk *)calloc(1, sizeof(*vk));
	if (!vk) {
		seterr(err, errlen, "oom", VK_SUCCESS);
		return -1;
	}
	vk->mode = mode;
	vk->width = prefer_w;
	vk->height = prefer_h;
	int acquire = mode == WORLDR_VK_DISPLAY && drm_fd >= 0 && connector_id != 0;
	if (create_instance(mode, acquire, &vk->instance, err, errlen) != 0) {
		free(vk);
		return -1;
	}
	if (pick_phys(vk->instance, mode, &vk->phys, &vk->queue_family, err, errlen) != 0) {
		vkDestroyInstance(vk->instance, NULL);
		free(vk);
		return -1;
	}
	VkPhysicalDeviceProperties props;
	vkGetPhysicalDeviceProperties(vk->phys, &props);
	vk->vendor_id = props.vendorID;
	snprintf(vk->device_name, sizeof(vk->device_name), "%s", props.deviceName);

	if (acquire && acquire_drm_display(vk, drm_fd, connector_id, err, errlen) != 0) {
		vkDestroyInstance(vk->instance, NULL);
		free(vk);
		return -1;
	}

	if (create_device(vk, mode == WORLDR_VK_DISPLAY, err, errlen) != 0) {
		vkDestroyInstance(vk->instance, NULL);
		free(vk);
		return -1;
	}
	if (mode == WORLDR_VK_DISPLAY) {
		if (create_display_surface(vk, prefer_w, prefer_h, err, errlen) != 0 ||
		    create_swapchain(vk, err, errlen) != 0 ||
		    create_cmds(vk, err, errlen) != 0) {
			worldr_vk_destroy(vk);
			return -1;
		}
	} else {
		if (create_headless_image(vk, err, errlen) != 0 || create_cmds(vk, err, errlen) != 0) {
			worldr_vk_destroy(vk);
			return -1;
		}
	}
	*out = vk;
	return 0;
}

int worldr_vk_create(int mode, uint32_t prefer_w, uint32_t prefer_h, worldr_vk **out, char *err, int errlen)
{
	return vk_create_ex(mode, prefer_w, prefer_h, -1, 0, out, err, errlen);
}

int worldr_vk_create_on_drm(int drm_fd, uint32_t connector_id, uint32_t prefer_w, uint32_t prefer_h,
			    worldr_vk **out, char *err, int errlen)
{
	if (drm_fd < 0 || connector_id == 0) {
		seterr(err, errlen, "VK_EXT_acquire_drm_display needs a master DRM fd and connector", VK_SUCCESS);
		return -1;
	}
	return vk_create_ex(WORLDR_VK_DISPLAY, prefer_w, prefer_h, drm_fd, connector_id, out, err, errlen);
}

void worldr_vk_destroy(worldr_vk *vk)
{
	if (!vk) {
		return;
	}
	if (vk->device) {
		/* Bounded wait: UINT64_MAX WaitIdle can hang the spare VT forever. */
		if (vk->fence) {
			vkWaitForFences(vk->device, 1, &vk->fence, VK_TRUE, WORLDR_VK_WAIT_NS);
		} else {
			vkDeviceWaitIdle(vk->device);
		}
	}
	if (vk->staging_map && vk->staging_mem) {
		vkUnmapMemory(vk->device, vk->staging_mem);
	}
	if (vk->staging) {
		vkDestroyBuffer(vk->device, vk->staging, NULL);
	}
	if (vk->staging_mem) {
		vkFreeMemory(vk->device, vk->staging_mem, NULL);
	}
	if (vk->headless_image) {
		vkDestroyImage(vk->device, vk->headless_image, NULL);
	}
	if (vk->headless_mem) {
		vkFreeMemory(vk->device, vk->headless_mem, NULL);
	}
	if (vk->img_avail) {
		vkDestroySemaphore(vk->device, vk->img_avail, NULL);
	}
	if (vk->done) {
		vkDestroySemaphore(vk->device, vk->done, NULL);
	}
	if (vk->fence) {
		vkDestroyFence(vk->device, vk->fence, NULL);
	}
	if (vk->device) {
		for (int i = 0; i < WORLDR_DMABUF_SLOTS; i++) {
			if (!vk->dma[i].used) {
				continue;
			}
			if (vk->dma[i].image) {
				vkDestroyImage(vk->device, vk->dma[i].image, NULL);
			}
			for (int m = 0; m < 4; m++) {
				if (vk->dma[i].mem[m]) {
					vkFreeMemory(vk->device, vk->dma[i].mem[m], NULL);
				}
			}
			memset(&vk->dma[i], 0, sizeof(vk->dma[i]));
		}
	}
	if (vk->cmd_pool) {
		vkDestroyCommandPool(vk->device, vk->cmd_pool, NULL);
	}
	if (vk->swapchain) {
		vkDestroySwapchainKHR(vk->device, vk->swapchain, NULL);
	}
	if (vk->device) {
		vkDestroyDevice(vk->device, NULL);
	}
	if (vk->surface) {
		vkDestroySurfaceKHR(vk->instance, vk->surface, NULL);
	}
	if (vk->instance) {
		vkDestroyInstance(vk->instance, NULL);
	}
	free(vk);
}

const char *worldr_vk_device_name(const worldr_vk *vk)
{
	return vk ? vk->device_name : "";
}

uint32_t worldr_vk_width(const worldr_vk *vk)
{
	return vk ? vk->width : 0;
}

uint32_t worldr_vk_height(const worldr_vk *vk)
{
	return vk ? vk->height : 0;
}

uint32_t worldr_vk_vendor_id(const worldr_vk *vk)
{
	return vk ? vk->vendor_id : 0;
}

int worldr_vk_list_devices(char *out, int outlen, char *err, int errlen)
{
	VkInstance inst;
	if (create_instance(WORLDR_VK_HEADLESS, 0, &inst, err, errlen) != 0) {
		return -1;
	}
	uint32_t n = 0;
	vkEnumeratePhysicalDevices(inst, &n, NULL);
	if (n == 0) {
		vkDestroyInstance(inst, NULL);
		if (out && outlen > 0) {
			snprintf(out, (size_t)outlen, "(no physical devices)\n");
		}
		return 0;
	}
	VkPhysicalDevice *devs = (VkPhysicalDevice *)calloc(n, sizeof(*devs));
	if (!devs) {
		vkDestroyInstance(inst, NULL);
		seterr(err, errlen, "oom", VK_SUCCESS);
		return -1;
	}
	vkEnumeratePhysicalDevices(inst, &n, devs);
	int used = 0;
	for (uint32_t i = 0; i < n && out && outlen > 1; i++) {
		VkPhysicalDeviceProperties p;
		vkGetPhysicalDeviceProperties(devs[i], &p);
		int w = snprintf(out + used, (size_t)(outlen - used),
				 "[%u] %s vendor=0x%04x device=0x%04x api=%u.%u type=%u\n",
				 i, p.deviceName, p.vendorID, p.deviceID,
				 VK_VERSION_MAJOR(p.apiVersion), VK_VERSION_MINOR(p.apiVersion),
				 (unsigned)p.deviceType);
		if (w < 0) {
			break;
		}
		used += w;
		if (used >= outlen) {
			used = outlen - 1;
			break;
		}
	}
	free(devs);
	vkDestroyInstance(inst, NULL);
	return 0;
}

static int record_clear(worldr_vk *vk, VkImage image, float r, float g, float b, float a, int to_present)
{
	VkCommandBufferBeginInfo bi = {0};
	bi.sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO;
	bi.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT;
	vkResetCommandBuffer(vk->cmd, 0);
	if (vkBeginCommandBuffer(vk->cmd, &bi) != VK_SUCCESS) {
		return -1;
	}
	barrier(vk->cmd, image, VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
		0, VK_ACCESS_TRANSFER_WRITE_BIT, VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);
	VkClearColorValue cc;
	memset(&cc, 0, sizeof(cc));
	cc.float32[0] = r;
	cc.float32[1] = g;
	cc.float32[2] = b;
	cc.float32[3] = a;
	VkImageSubresourceRange rng = {0};
	rng.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
	rng.levelCount = 1;
	rng.layerCount = 1;
	vkCmdClearColorImage(vk->cmd, image, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, &cc, 1, &rng);
	if (to_present) {
		barrier(vk->cmd, image, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_PRESENT_SRC_KHR,
			VK_ACCESS_TRANSFER_WRITE_BIT, 0, VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT);
	} else {
		barrier(vk->cmd, image, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_GENERAL,
			VK_ACCESS_TRANSFER_WRITE_BIT, VK_ACCESS_HOST_READ_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_HOST_BIT);
	}
	return vkEndCommandBuffer(vk->cmd) == VK_SUCCESS ? 0 : -1;
}

int worldr_vk_clear_present(worldr_vk *vk, float r, float g, float b, float a, char *err, int errlen)
{
	if (!vk || vk->mode != WORLDR_VK_DISPLAY) {
		seterr(err, errlen, "clear_present requires vk-display session", VK_SUCCESS);
		return -1;
	}
	if (vkWaitForFences(vk->device, 1, &vk->fence, VK_TRUE, WORLDR_VK_WAIT_NS) == VK_TIMEOUT) {
		seterr(err, errlen, "GPU wait timed out (2s) before acquire — lost DRM master? Spare TTY: pkill worldr-shell, Ctrl+Alt+F1", VK_TIMEOUT);
		return -1;
	}
	vkResetFences(vk->device, 1, &vk->fence);
	uint32_t idx = 0;
	VkResult ar = vkAcquireNextImageKHR(vk->device, vk->swapchain, WORLDR_VK_WAIT_NS, vk->img_avail, VK_NULL_HANDLE, &idx);
	if (ar == VK_TIMEOUT) {
		seterr(err, errlen, "vkAcquireNextImageKHR timed out (2s) — not DRM master or display gone. Spare TTY: scripts/try-tty.sh", ar);
		return -1;
	}
	if (ar != VK_SUCCESS && ar != VK_SUBOPTIMAL_KHR) {
		seterr(err, errlen, "vkAcquireNextImageKHR failed", ar);
		return -1;
	}
	if (record_clear(vk, vk->images[idx], r, g, b, a, 1) != 0) {
		seterr(err, errlen, "record clear failed", VK_ERROR_UNKNOWN);
		return -1;
	}
	if (submit_wait(vk, vk->img_avail, VK_PIPELINE_STAGE_TRANSFER_BIT, vk->done, err, errlen) != 0) {
		return -1;
	}
	VkPresentInfoKHR pi = {0};
	pi.sType = VK_STRUCTURE_TYPE_PRESENT_INFO_KHR;
	pi.waitSemaphoreCount = 1;
	pi.pWaitSemaphores = &vk->done;
	pi.swapchainCount = 1;
	pi.pSwapchains = &vk->swapchain;
	pi.pImageIndices = &idx;
	VkResult pr = vkQueuePresentKHR(vk->queue, &pi);
	if (pr != VK_SUCCESS && pr != VK_SUBOPTIMAL_KHR) {
		seterr(err, errlen, "vkQueuePresentKHR failed", pr);
		return -1;
	}
	return 0;
}

int worldr_vk_upload_present(worldr_vk *vk, const uint8_t *bgra, uint32_t stride, char *err, int errlen)
{
	return worldr_vk_upload_present_layers(vk, bgra, stride, NULL, 0, err, errlen);
}

int worldr_vk_headless_clear(worldr_vk *vk, float r, float g, float b, float a, uint32_t *out_pixel, char *err, int errlen)
{
	if (!vk || vk->mode != WORLDR_VK_HEADLESS) {
		seterr(err, errlen, "headless_clear requires headless session", VK_SUCCESS);
		return -1;
	}
	if (record_clear(vk, vk->headless_image, r, g, b, a, 0) != 0) {
		seterr(err, errlen, "record headless clear failed", VK_ERROR_UNKNOWN);
		return -1;
	}
	if (submit_wait(vk, VK_NULL_HANDLE, 0, VK_NULL_HANDLE, err, errlen) != 0) {
		return -1;
	}
	if (out_pixel) {
		*out_pixel = 0;
		void *map = NULL;
		if (vkMapMemory(vk->device, vk->headless_mem, 0, VK_WHOLE_SIZE, 0, &map) == VK_SUCCESS && map) {
			memcpy(out_pixel, map, 4);
			vkUnmapMemory(vk->device, vk->headless_mem);
		}
	}
	return 0;
}

int worldr_vk_has_dmabuf(const worldr_vk *vk)
{
	return vk && vk->dmabuf_ok && vk->device;
}

int worldr_vk_is_display(const worldr_vk *vk)
{
	return vk && vk->mode == WORLDR_VK_DISPLAY && vk->device;
}

int worldr_vk_has_timeline(const worldr_vk *vk)
{
	return vk && vk->device && vk->timeline_ok && vk->ext_sem_fd;
}

uint32_t worldr_vk_display_planes(const worldr_vk *vk)
{
	return vk ? vk->display_planes : 0;
}

int worldr_vk_wait_timeline_fd(worldr_vk *vk, int fd, uint64_t point, uint64_t timeout_ns, char *err, int errlen)
{
	if (!vk || !vk->device || fd < 0 || !worldr_vk_has_timeline(vk)) {
		seterr(err, errlen, "vulkan timeline wait unavailable", VK_SUCCESS);
		return -1;
	}
	int dupfd = dup(fd);
	if (dupfd < 0) {
		seterr(err, errlen, "dup syncobj fd", VK_SUCCESS);
		return -1;
	}

	VkSemaphoreTypeCreateInfo tci = {0};
	tci.sType = VK_STRUCTURE_TYPE_SEMAPHORE_TYPE_CREATE_INFO;
	tci.semaphoreType = VK_SEMAPHORE_TYPE_TIMELINE;
	tci.initialValue = 0;
	VkSemaphoreCreateInfo sci = {0};
	sci.sType = VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO;
	sci.pNext = &tci;
	VkSemaphore sem = VK_NULL_HANDLE;
	VkResult r = vkCreateSemaphore(vk->device, &sci, NULL, &sem);
	if (r != VK_SUCCESS) {
		close(dupfd);
		seterr(err, errlen, "vkCreateSemaphore timeline", r);
		return -1;
	}

	PFN_vkImportSemaphoreFdKHR import_fd =
		(PFN_vkImportSemaphoreFdKHR)vkGetDeviceProcAddr(vk->device, "vkImportSemaphoreFdKHR");
	if (!import_fd) {
		vkDestroySemaphore(vk->device, sem, NULL);
		close(dupfd);
		seterr(err, errlen, "vkImportSemaphoreFdKHR missing", VK_SUCCESS);
		return -1;
	}
	VkImportSemaphoreFdInfoKHR imp = {0};
	imp.sType = VK_STRUCTURE_TYPE_IMPORT_SEMAPHORE_FD_INFO_KHR;
	imp.semaphore = sem;
	imp.flags = VK_SEMAPHORE_IMPORT_TEMPORARY_BIT;
	imp.handleType = VK_EXTERNAL_SEMAPHORE_HANDLE_TYPE_OPAQUE_FD_BIT;
	imp.fd = dupfd;
	r = import_fd(vk->device, &imp);
	if (r != VK_SUCCESS) {
		/* Import takes the fd only on success. */
		close(dupfd);
		vkDestroySemaphore(vk->device, sem, NULL);
		seterr(err, errlen, "vkImportSemaphoreFdKHR", r);
		return -1;
	}

	if (timeout_ns == 0) {
		timeout_ns = 100000000ull;
	}
	VkSemaphoreWaitInfo wi = {0};
	wi.sType = VK_STRUCTURE_TYPE_SEMAPHORE_WAIT_INFO;
	wi.semaphoreCount = 1;
	wi.pSemaphores = &sem;
	wi.pValues = &point;
	r = vkWaitSemaphores(vk->device, &wi, timeout_ns);
	vkDestroySemaphore(vk->device, sem, NULL);
	if (r == VK_TIMEOUT) {
		seterr(err, errlen, "vkWaitSemaphores timeline timeout", r);
		return -1;
	}
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkWaitSemaphores timeline", r);
		return -1;
	}
	return 0;
}

static VkFormat fourcc_to_vk(uint32_t fourcc, int *swizzle_rb)
{
	*swizzle_rb = 0;
	switch (fourcc) {
	case DRM_FORMAT_XRGB8888:
	case DRM_FORMAT_ARGB8888:
		return VK_FORMAT_B8G8R8A8_UNORM;
	case DRM_FORMAT_XBGR8888:
	case DRM_FORMAT_ABGR8888:
		*swizzle_rb = 1;
		return VK_FORMAT_R8G8B8A8_UNORM;
	default:
		return VK_FORMAT_UNDEFINED;
	}
}

static void dma_slot_reset(worldr_vk *vk, worldr_dma_slot *s)
{
	if (!vk || !s || !s->used) {
		return;
	}
	if (s->image) {
		vkDestroyImage(vk->device, s->image, NULL);
	}
	for (int i = 0; i < 4; i++) {
		if (s->mem[i]) {
			vkFreeMemory(vk->device, s->mem[i], NULL);
		}
	}
	memset(s, 0, sizeof(*s));
}

/* Import dma-buf as a TRANSFER_SRC VkImage. Caller owns image + mem[0..nmem). */
static int dma_bind_image(worldr_vk *vk, uint32_t width, uint32_t height, uint32_t fourcc, uint64_t modifier,
			  int nplanes, const int *fds, const uint32_t *offsets, const uint32_t *pitches,
			  VkImage *out_img, VkDeviceMemory mem[4], int *out_nmem, VkFormat *out_fmt, char *err, int errlen)
{
	int swizzle = 0;
	VkFormat fmt = fourcc_to_vk(fourcc, &swizzle);
	if (fmt == VK_FORMAT_UNDEFINED) {
		seterr(err, errlen, "unsupported dmabuf fourcc (want ARGB/XRGB/ABGR/XBGR8888)", VK_SUCCESS);
		return -1;
	}
	int use_mod = has_dev_ext(vk->phys, VK_EXT_IMAGE_DRM_FORMAT_MODIFIER_EXTENSION_NAME) &&
		      modifier != DRM_FORMAT_MOD_INVALID;
	if (modifier == DRM_FORMAT_MOD_LINEAR) {
		use_mod = 0;
	}

	VkImageDrmFormatModifierExplicitCreateInfoEXT expl = {0};
	VkSubresourceLayout layouts[4];
	memset(layouts, 0, sizeof(layouts));
	for (int i = 0; i < nplanes; i++) {
		layouts[i].offset = offsets[i];
		layouts[i].rowPitch = pitches[i];
	}
	expl.sType = VK_STRUCTURE_TYPE_IMAGE_DRM_FORMAT_MODIFIER_EXPLICIT_CREATE_INFO_EXT;
	expl.drmFormatModifier = modifier;
	expl.drmFormatModifierPlaneCount = (uint32_t)nplanes;
	expl.pPlaneLayouts = layouts;

	VkExternalMemoryImageCreateInfo ext = {0};
	ext.sType = VK_STRUCTURE_TYPE_EXTERNAL_MEMORY_IMAGE_CREATE_INFO;
	ext.handleTypes = VK_EXTERNAL_MEMORY_HANDLE_TYPE_DMA_BUF_BIT_EXT;
	if (use_mod) {
		ext.pNext = &expl;
	}

	VkImageCreateInfo ii = {0};
	ii.sType = VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO;
	ii.pNext = &ext;
	ii.imageType = VK_IMAGE_TYPE_2D;
	ii.format = fmt;
	ii.extent.width = width;
	ii.extent.height = height;
	ii.extent.depth = 1;
	ii.mipLevels = 1;
	ii.arrayLayers = 1;
	ii.samples = VK_SAMPLE_COUNT_1_BIT;
	ii.tiling = use_mod ? VK_IMAGE_TILING_DRM_FORMAT_MODIFIER_EXT : VK_IMAGE_TILING_LINEAR;
	ii.usage = VK_IMAGE_USAGE_TRANSFER_SRC_BIT;
	ii.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
	ii.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;

	VkImage src = VK_NULL_HANDLE;
	VkResult r = vkCreateImage(vk->device, &ii, NULL, &src);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkCreateImage dmabuf failed (Intel tiled modifiers need VK_EXT_image_drm_format_modifier)", r);
		return -1;
	}

	int imported = 0;
	if (use_mod && nplanes > 1) {
		for (int i = 0; i < nplanes; i++) {
			VkImagePlaneMemoryRequirementsInfo preq = {0};
			preq.sType = VK_STRUCTURE_TYPE_IMAGE_PLANE_MEMORY_REQUIREMENTS_INFO;
			preq.planeAspect = (VkImageAspectFlagBits)(VK_IMAGE_ASPECT_MEMORY_PLANE_0_BIT_EXT << i);
			VkImageMemoryRequirementsInfo2 in2 = {0};
			in2.sType = VK_STRUCTURE_TYPE_IMAGE_MEMORY_REQUIREMENTS_INFO_2;
			in2.pNext = &preq;
			in2.image = src;
			VkMemoryRequirements2 mr2 = {.sType = VK_STRUCTURE_TYPE_MEMORY_REQUIREMENTS_2};
			vkGetImageMemoryRequirements2(vk->device, &in2, &mr2);
			uint32_t mi = find_memory(vk->phys, mr2.memoryRequirements.memoryTypeBits, 0);
			if (mi == UINT32_MAX) {
				mi = 0;
			}
			int dfd = dup(fds[i]);
			if (dfd < 0) {
				seterr(err, errlen, "dup dmabuf fd", VK_SUCCESS);
				goto fail;
			}
			VkImportMemoryFdInfoKHR imp = {0};
			imp.sType = VK_STRUCTURE_TYPE_IMPORT_MEMORY_FD_INFO_KHR;
			imp.handleType = VK_EXTERNAL_MEMORY_HANDLE_TYPE_DMA_BUF_BIT_EXT;
			imp.fd = dfd;
			VkMemoryDedicatedAllocateInfo ded = {0};
			ded.sType = VK_STRUCTURE_TYPE_MEMORY_DEDICATED_ALLOCATE_INFO;
			ded.pNext = &imp;
			ded.image = src;
			VkMemoryAllocateInfo ai = {0};
			ai.sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO;
			ai.pNext = &ded;
			ai.allocationSize = mr2.memoryRequirements.size;
			ai.memoryTypeIndex = mi;
			r = vkAllocateMemory(vk->device, &ai, NULL, &mem[i]);
			if (r != VK_SUCCESS) {
				close(dfd);
				seterr(err, errlen, "vkAllocateMemory import plane failed", r);
				goto fail;
			}
			imported++;
			VkBindImagePlaneMemoryInfo bp = {0};
			bp.sType = VK_STRUCTURE_TYPE_BIND_IMAGE_PLANE_MEMORY_INFO;
			bp.planeAspect = preq.planeAspect;
			VkBindImageMemoryInfo bi = {0};
			bi.sType = VK_STRUCTURE_TYPE_BIND_IMAGE_MEMORY_INFO;
			bi.pNext = &bp;
			bi.image = src;
			bi.memory = mem[i];
			r = vkBindImageMemory2(vk->device, 1, &bi);
			if (r != VK_SUCCESS) {
				seterr(err, errlen, "vkBindImageMemory2 plane failed", r);
				goto fail;
			}
		}
	} else {
		VkMemoryRequirements mr;
		vkGetImageMemoryRequirements(vk->device, src, &mr);
		uint32_t mi = find_memory(vk->phys, mr.memoryTypeBits, 0);
		if (mi == UINT32_MAX) {
			mi = 0;
		}
		int dfd = dup(fds[0]);
		if (dfd < 0) {
			seterr(err, errlen, "dup dmabuf fd", VK_SUCCESS);
			goto fail;
		}
		VkImportMemoryFdInfoKHR imp = {0};
		imp.sType = VK_STRUCTURE_TYPE_IMPORT_MEMORY_FD_INFO_KHR;
		imp.handleType = VK_EXTERNAL_MEMORY_HANDLE_TYPE_DMA_BUF_BIT_EXT;
		imp.fd = dfd;
		VkMemoryDedicatedAllocateInfo ded = {0};
		ded.sType = VK_STRUCTURE_TYPE_MEMORY_DEDICATED_ALLOCATE_INFO;
		ded.pNext = &imp;
		ded.image = src;
		VkMemoryAllocateInfo ai = {0};
		ai.sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO;
		ai.pNext = &ded;
		ai.allocationSize = mr.size;
		ai.memoryTypeIndex = mi;
		r = vkAllocateMemory(vk->device, &ai, NULL, &mem[0]);
		if (r != VK_SUCCESS) {
			close(dfd);
			seterr(err, errlen, "vkAllocateMemory import failed", r);
			goto fail;
		}
		imported = 1;
		r = vkBindImageMemory(vk->device, src, mem[0], 0);
		if (r != VK_SUCCESS) {
			seterr(err, errlen, "vkBindImageMemory import failed", r);
			goto fail;
		}
	}
	*out_img = src;
	*out_nmem = imported;
	*out_fmt = fmt;
	(void)swizzle;
	return 0;

fail:
	vkDestroyImage(vk->device, src, NULL);
	for (int i = 0; i < 4; i++) {
		if (mem[i]) {
			vkFreeMemory(vk->device, mem[i], NULL);
			mem[i] = VK_NULL_HANDLE;
		}
	}
	return -1;
}

int worldr_vk_dmabuf_import(worldr_vk *vk, uint32_t width, uint32_t height, uint32_t fourcc, uint64_t modifier,
			    int nplanes, const int *fds, const uint32_t *offsets, const uint32_t *pitches,
			    uint8_t *out_bgra, uint32_t out_stride, char *err, int errlen)
{
	if (!vk || !vk->device || !out_bgra || nplanes < 1 || nplanes > 4 || !fds) {
		seterr(err, errlen, "dmabuf import: bad args", VK_SUCCESS);
		return -1;
	}
	if (!vk->dmabuf_ok) {
		seterr(err, errlen, "device lacks VK_EXT_external_memory_dma_buf + VK_KHR_external_memory_fd", VK_SUCCESS);
		return -1;
	}
	if (vk->fence) {
		if (vkWaitForFences(vk->device, 1, &vk->fence, VK_TRUE, WORLDR_VK_WAIT_NS) == VK_TIMEOUT) {
			seterr(err, errlen, "GPU wait timed out (2s) before dmabuf import", VK_TIMEOUT);
			return -1;
		}
		vkResetFences(vk->device, 1, &vk->fence);
	}

	int swizzle = 0;
	VkFormat fmt = fourcc_to_vk(fourcc, &swizzle);
	VkImage src = VK_NULL_HANDLE;
	VkDeviceMemory plane_mem[4] = {0};
	int imported = 0;
	if (dma_bind_image(vk, width, height, fourcc, modifier, nplanes, fds, offsets, pitches,
			   &src, plane_mem, &imported, &fmt, err, errlen) != 0) {
		return -1;
	}
	(void)fourcc_to_vk(fourcc, &swizzle);
	VkResult r;

	VkImageCreateInfo li = {0};
	li.sType = VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO;
	li.imageType = VK_IMAGE_TYPE_2D;
	li.format = fmt;
	li.extent.width = width;
	li.extent.height = height;
	li.extent.depth = 1;
	li.mipLevels = 1;
	li.arrayLayers = 1;
	li.samples = VK_SAMPLE_COUNT_1_BIT;
	li.tiling = VK_IMAGE_TILING_LINEAR;
	li.usage = VK_IMAGE_USAGE_TRANSFER_DST_BIT;
	li.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
	li.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
	VkImage dst = VK_NULL_HANDLE;
	r = vkCreateImage(vk->device, &li, NULL, &dst);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkCreateImage linear readback failed", r);
		goto fail;
	}
	VkMemoryRequirements lmr;
	vkGetImageMemoryRequirements(vk->device, dst, &lmr);
	uint32_t lmi = find_memory(vk->phys, lmr.memoryTypeBits, VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT);
	if (lmi == UINT32_MAX) {
		seterr(err, errlen, "no host-visible memory for dmabuf readback", VK_SUCCESS);
		vkDestroyImage(vk->device, dst, NULL);
		goto fail;
	}
	VkDeviceMemory dstmem = VK_NULL_HANDLE;
	VkMemoryAllocateInfo lai = {0};
	lai.sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO;
	lai.allocationSize = lmr.size;
	lai.memoryTypeIndex = lmi;
	r = vkAllocateMemory(vk->device, &lai, NULL, &dstmem);
	if (r != VK_SUCCESS) {
		vkDestroyImage(vk->device, dst, NULL);
		seterr(err, errlen, "allocate linear readback failed", r);
		goto fail;
	}
	vkBindImageMemory(vk->device, dst, dstmem, 0);

	VkCommandBufferBeginInfo bi = {0};
	bi.sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO;
	bi.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT;
	vkResetCommandBuffer(vk->cmd, 0);
	if (vkBeginCommandBuffer(vk->cmd, &bi) != VK_SUCCESS) {
		seterr(err, errlen, "begin dmabuf copy cmd failed", VK_ERROR_UNKNOWN);
		vkDestroyImage(vk->device, dst, NULL);
		vkFreeMemory(vk->device, dstmem, NULL);
		goto fail;
	}
	barrier(vk->cmd, src, VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
		0, VK_ACCESS_TRANSFER_READ_BIT, VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);
	barrier(vk->cmd, dst, VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
		0, VK_ACCESS_TRANSFER_WRITE_BIT, VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);
	VkImageCopy cpy = {0};
	cpy.srcSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
	cpy.srcSubresource.layerCount = 1;
	cpy.dstSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
	cpy.dstSubresource.layerCount = 1;
	cpy.extent.width = width;
	cpy.extent.height = height;
	cpy.extent.depth = 1;
	vkCmdCopyImage(vk->cmd, src, VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL, dst, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &cpy);
	barrier(vk->cmd, dst, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_GENERAL,
		VK_ACCESS_TRANSFER_WRITE_BIT, VK_ACCESS_HOST_READ_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_HOST_BIT);
	vkEndCommandBuffer(vk->cmd);
	if (submit_wait(vk, VK_NULL_HANDLE, 0, VK_NULL_HANDLE, err, errlen) != 0) {
		vkDestroyImage(vk->device, dst, NULL);
		vkFreeMemory(vk->device, dstmem, NULL);
		goto fail;
	}

	VkImageSubresource sub = {.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT};
	VkSubresourceLayout lay;
	vkGetImageSubresourceLayout(vk->device, dst, &sub, &lay);
	void *map = NULL;
	r = vkMapMemory(vk->device, dstmem, 0, VK_WHOLE_SIZE, 0, &map);
	if (r != VK_SUCCESS || !map) {
		seterr(err, errlen, "map linear readback failed", r);
		vkDestroyImage(vk->device, dst, NULL);
		vkFreeMemory(vk->device, dstmem, NULL);
		goto fail;
	}
	const uint8_t *srcp = (const uint8_t *)map + lay.offset;
	for (uint32_t y = 0; y < height; y++) {
		const uint8_t *row = srcp + y * lay.rowPitch;
		uint8_t *dstp = out_bgra + y * out_stride;
		if (!swizzle) {
			memcpy(dstp, row, width * 4);
		} else {
			for (uint32_t x = 0; x < width; x++) {
				dstp[x * 4 + 0] = row[x * 4 + 2];
				dstp[x * 4 + 1] = row[x * 4 + 1];
				dstp[x * 4 + 2] = row[x * 4 + 0];
				dstp[x * 4 + 3] = row[x * 4 + 3];
			}
		}
	}
	vkUnmapMemory(vk->device, dstmem);
	vkDestroyImage(vk->device, dst, NULL);
	vkFreeMemory(vk->device, dstmem, NULL);
	vkDestroyImage(vk->device, src, NULL);
	for (int i = 0; i < imported; i++) {
		if (plane_mem[i]) {
			vkFreeMemory(vk->device, plane_mem[i], NULL);
		}
	}
	return 0;

fail:
	vkDestroyImage(vk->device, src, NULL);
	for (int i = 0; i < 4; i++) {
		if (plane_mem[i]) {
			vkFreeMemory(vk->device, plane_mem[i], NULL);
		}
	}
	return -1;
}

int worldr_vk_dmabuf_retain(worldr_vk *vk, uint32_t width, uint32_t height, uint32_t fourcc, uint64_t modifier,
			    int nplanes, const int *fds, const uint32_t *offsets, const uint32_t *pitches,
			    int *out_slot, char *err, int errlen)
{
	if (!vk || !vk->device || !out_slot || nplanes < 1 || nplanes > 4 || !fds) {
		seterr(err, errlen, "dmabuf retain: bad args", VK_SUCCESS);
		return -1;
	}
	if (!vk->dmabuf_ok) {
		seterr(err, errlen, "device lacks VK_EXT_external_memory_dma_buf + VK_KHR_external_memory_fd", VK_SUCCESS);
		return -1;
	}
	int idx = -1;
	for (int i = 0; i < WORLDR_DMABUF_SLOTS; i++) {
		if (!vk->dma[i].used) {
			idx = i;
			break;
		}
	}
	if (idx < 0) {
		seterr(err, errlen, "dmabuf retain: no free GPU slots", VK_SUCCESS);
		return -1;
	}
	worldr_dma_slot *s = &vk->dma[idx];
	memset(s, 0, sizeof(*s));
	if (dma_bind_image(vk, width, height, fourcc, modifier, nplanes, fds, offsets, pitches,
			   &s->image, s->mem, &s->nmem, &s->fmt, err, errlen) != 0) {
		memset(s, 0, sizeof(*s));
		return -1;
	}
	s->used = 1;
	s->w = width;
	s->h = height;
	*out_slot = idx + 1;
	return 0;
}

void worldr_vk_dmabuf_release(worldr_vk *vk, int slot)
{
	if (!vk || slot < 1 || slot > WORLDR_DMABUF_SLOTS) {
		return;
	}
	dma_slot_reset(vk, &vk->dma[slot - 1]);
}

int worldr_vk_upload_present_layers(worldr_vk *vk, const uint8_t *bgra, uint32_t stride,
				    const worldr_vk_layer *layers, int nlayers, char *err, int errlen)
{
	if (!vk || vk->mode != WORLDR_VK_DISPLAY || !bgra) {
		seterr(err, errlen, "upload_present requires vk-display + pixels", VK_SUCCESS);
		return -1;
	}
	VkDeviceSize size = (VkDeviceSize)stride * vk->height;
	if (ensure_staging(vk, size, err, errlen) != 0) {
		return -1;
	}
	memcpy(vk->staging_map, bgra, (size_t)size);
	if (vkWaitForFences(vk->device, 1, &vk->fence, VK_TRUE, WORLDR_VK_WAIT_NS) == VK_TIMEOUT) {
		seterr(err, errlen, "GPU wait timed out (2s) before upload — lost DRM master? Spare TTY: pkill worldr-shell, Ctrl+Alt+F1", VK_TIMEOUT);
		return -1;
	}
	vkResetFences(vk->device, 1, &vk->fence);
	uint32_t idx = 0;
	VkResult ar = vkAcquireNextImageKHR(vk->device, vk->swapchain, WORLDR_VK_WAIT_NS, vk->img_avail, VK_NULL_HANDLE, &idx);
	if (ar == VK_TIMEOUT) {
		seterr(err, errlen, "vkAcquireNextImageKHR timed out (2s) — not DRM master or display gone. Spare TTY: scripts/try-tty.sh", ar);
		return -1;
	}
	if (ar != VK_SUCCESS && ar != VK_SUBOPTIMAL_KHR) {
		seterr(err, errlen, "vkAcquireNextImageKHR failed", ar);
		return -1;
	}
	VkCommandBufferBeginInfo bi = {0};
	bi.sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO;
	bi.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT;
	vkResetCommandBuffer(vk->cmd, 0);
	if (vkBeginCommandBuffer(vk->cmd, &bi) != VK_SUCCESS) {
		seterr(err, errlen, "begin cmd failed", VK_ERROR_UNKNOWN);
		return -1;
	}
	barrier(vk->cmd, vk->images[idx], VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
		0, VK_ACCESS_TRANSFER_WRITE_BIT, VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);
	VkBufferImageCopy cpy = {0};
	cpy.imageSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
	cpy.imageSubresource.layerCount = 1;
	cpy.imageExtent.width = vk->width;
	cpy.imageExtent.height = vk->height;
	cpy.imageExtent.depth = 1;
	cpy.bufferRowLength = stride / 4;
	vkCmdCopyBufferToImage(vk->cmd, vk->staging, vk->images[idx], VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &cpy);
	/* Implicit sync on Intel: layout transition waits on the dma-buf reservation. */
	if (layers && nlayers > 0) {
		for (int i = 0; i < nlayers; i++) {
			int slot = layers[i].slot;
			if (slot < 1 || slot > WORLDR_DMABUF_SLOTS || !vk->dma[slot - 1].used) {
				continue;
			}
			worldr_dma_slot *s = &vk->dma[slot - 1];
			if (s->fmt != VK_FORMAT_B8G8R8A8_UNORM && s->fmt != vk->format) {
				continue; /* ABGR sampled only via CPU readback */
			}
			int32_t dx = layers[i].x, dy = layers[i].y, dw = layers[i].w, dh = layers[i].h;
			if (dw <= 0 || dh <= 0) {
				continue;
			}
			barrier(vk->cmd, s->image, VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
				0, VK_ACCESS_TRANSFER_READ_BIT, VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);
			VkImageBlit bl = {0};
			bl.srcSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
			bl.srcSubresource.layerCount = 1;
			bl.srcOffsets[1].x = (int32_t)s->w;
			bl.srcOffsets[1].y = (int32_t)s->h;
			bl.srcOffsets[1].z = 1;
			bl.dstSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
			bl.dstSubresource.layerCount = 1;
			bl.dstOffsets[0].x = dx;
			bl.dstOffsets[0].y = dy;
			bl.dstOffsets[1].x = dx + dw;
			bl.dstOffsets[1].y = dy + dh;
			bl.dstOffsets[1].z = 1;
			vkCmdBlitImage(vk->cmd, s->image, VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
				       vk->images[idx], VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &bl, VK_FILTER_NEAREST);
		}
	}
	barrier(vk->cmd, vk->images[idx], VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_PRESENT_SRC_KHR,
		VK_ACCESS_TRANSFER_WRITE_BIT, 0, VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT);
	if (vkEndCommandBuffer(vk->cmd) != VK_SUCCESS) {
		seterr(err, errlen, "end cmd failed", VK_ERROR_UNKNOWN);
		return -1;
	}
	if (submit_wait(vk, vk->img_avail, VK_PIPELINE_STAGE_TRANSFER_BIT, vk->done, err, errlen) != 0) {
		return -1;
	}
	VkPresentInfoKHR pi = {0};
	pi.sType = VK_STRUCTURE_TYPE_PRESENT_INFO_KHR;
	pi.waitSemaphoreCount = 1;
	pi.pWaitSemaphores = &vk->done;
	pi.swapchainCount = 1;
	pi.pSwapchains = &vk->swapchain;
	pi.pImageIndices = &idx;
	VkResult pr = vkQueuePresentKHR(vk->queue, &pi);
	if (pr != VK_SUCCESS && pr != VK_SUBOPTIMAL_KHR) {
		seterr(err, errlen, "vkQueuePresentKHR failed", pr);
		return -1;
	}
	return 0;
}
