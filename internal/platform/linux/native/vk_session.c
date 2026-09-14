#define VK_USE_PLATFORM_DISPLAY_KHR
#include "vk_session.h"

#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <vulkan/vulkan.h>

#define WORLDR_MAX_IMAGES 8

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
};

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

static int create_instance(int mode, VkInstance *out, char *err, int errlen)
{
	const char *exts_display[] = {
		VK_KHR_SURFACE_EXTENSION_NAME,
		VK_KHR_DISPLAY_EXTENSION_NAME,
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
		ci.enabledExtensionCount = 2;
		ci.ppEnabledExtensionNames = exts_display;
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

static int create_device(worldr_vk *vk, int need_swapchain, char *err, int errlen)
{
	float prio = 1.0f;
	VkDeviceQueueCreateInfo qci = {0};
	qci.sType = VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO;
	qci.queueFamilyIndex = vk->queue_family;
	qci.queueCount = 1;
	qci.pQueuePriorities = &prio;

	const char *sw = VK_KHR_SWAPCHAIN_EXTENSION_NAME;
	VkDeviceCreateInfo dci = {0};
	dci.sType = VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO;
	dci.queueCreateInfoCount = 1;
	dci.pQueueCreateInfos = &qci;
	if (need_swapchain) {
		dci.enabledExtensionCount = 1;
		dci.ppEnabledExtensionNames = &sw;
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
		seterr(err, errlen, "VK_KHR_display: no displays. Run on a spare VT as DRM master (see docs/RUN-ABOX.md)", r);
		return -1;
	}
	VkDisplayPropertiesKHR *disps = (VkDisplayPropertiesKHR *)calloc(ndisp, sizeof(*disps));
	if (!disps) {
		seterr(err, errlen, "oom", VK_SUCCESS);
		return -1;
	}
	vkGetPhysicalDeviceDisplayPropertiesKHR(vk->phys, &ndisp, disps);

	VkDisplayKHR display = disps[0].display;
	uint32_t nmodes = 0;
	vkGetDisplayModePropertiesKHR(vk->phys, display, &nmodes, NULL);
	if (nmodes == 0) {
		free(disps);
		seterr(err, errlen, "display has no modes", VK_SUCCESS);
		return -1;
	}
	VkDisplayModePropertiesKHR *modes = (VkDisplayModePropertiesKHR *)calloc(nmodes, sizeof(*modes));
	if (!modes) {
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

	uint32_t nplanes = 0;
	vkGetPhysicalDeviceDisplayPlanePropertiesKHR(vk->phys, &nplanes, NULL);
	VkDisplayPlanePropertiesKHR *planes = NULL;
	if (nplanes) {
		planes = (VkDisplayPlanePropertiesKHR *)calloc(nplanes, sizeof(*planes));
		if (planes) {
			vkGetPhysicalDeviceDisplayPlanePropertiesKHR(vk->phys, &nplanes, planes);
		}
	}
	uint32_t plane = 0;
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
		seterr(err, errlen, "vkCreateDisplayPlaneSurfaceKHR failed", r);
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
		r = vkWaitForFences(vk->device, 1, &vk->fence, VK_TRUE, UINT64_MAX);
		if (r != VK_SUCCESS) {
			seterr(err, errlen, "vkWaitForFences failed", r);
			return -1;
		}
	} else {
		vkQueueWaitIdle(vk->queue);
	}
	return 0;
}

int worldr_vk_create(int mode, uint32_t prefer_w, uint32_t prefer_h, worldr_vk **out, char *err, int errlen)
{
	worldr_vk *vk = (worldr_vk *)calloc(1, sizeof(*vk));
	if (!vk) {
		seterr(err, errlen, "oom", VK_SUCCESS);
		return -1;
	}
	vk->mode = mode;
	vk->width = prefer_w;
	vk->height = prefer_h;
	if (create_instance(mode, &vk->instance, err, errlen) != 0) {
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

void worldr_vk_destroy(worldr_vk *vk)
{
	if (!vk) {
		return;
	}
	if (vk->device) {
		vkDeviceWaitIdle(vk->device);
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
	if (create_instance(WORLDR_VK_HEADLESS, &inst, err, errlen) != 0) {
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
	vkWaitForFences(vk->device, 1, &vk->fence, VK_TRUE, UINT64_MAX);
	vkResetFences(vk->device, 1, &vk->fence);
	uint32_t idx = 0;
	VkResult ar = vkAcquireNextImageKHR(vk->device, vk->swapchain, UINT64_MAX, vk->img_avail, VK_NULL_HANDLE, &idx);
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
	if (!vk || vk->mode != WORLDR_VK_DISPLAY || !bgra) {
		seterr(err, errlen, "upload_present requires vk-display + pixels", VK_SUCCESS);
		return -1;
	}
	VkDeviceSize size = (VkDeviceSize)stride * vk->height;
	if (ensure_staging(vk, size, err, errlen) != 0) {
		return -1;
	}
	memcpy(vk->staging_map, bgra, (size_t)size);
	vkWaitForFences(vk->device, 1, &vk->fence, VK_TRUE, UINT64_MAX);
	vkResetFences(vk->device, 1, &vk->fence);
	uint32_t idx = 0;
	VkResult ar = vkAcquireNextImageKHR(vk->device, vk->swapchain, UINT64_MAX, vk->img_avail, VK_NULL_HANDLE, &idx);
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
