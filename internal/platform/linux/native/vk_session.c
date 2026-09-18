#define VK_USE_PLATFORM_DISPLAY_KHR
#define VK_USE_PLATFORM_WAYLAND_KHR
#include "vk_session.h"
#include "vk_frame.h"

#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <unistd.h>
#include <fcntl.h>
#include <poll.h>
#include <errno.h>
#include <time.h>
#include <vulkan/vulkan.h>

#define WORLDR_MAX_IMAGES 8
#define WORLDR_MEMORY_BUDGET (1024ull*1024*1024)
/* Finite GPU wait so a lost DRM master / hung KMS does not wedge the TTY. */
#define WORLDR_VK_WAIT_NS 2000000000ull

typedef struct worldr_scene worldr_scene;
static void scene_destroy(worldr_vk *vk);
static int scene_wait(worldr_vk *vk, char *err, int errlen);
static void scene_mark_failed(worldr_vk *vk);

struct worldr_vk {
	int mode;
	int needs_recovery;
	uint64_t memory_budget,memory_used,memory_peak;
	uint32_t memory_images,memory_buffers;
	VkInstance instance;
	VkPhysicalDevice phys;
	VkDevice device;
	VkQueue queue;
	uint32_t queue_family;
	VkSurfaceKHR surface;
	VkSwapchainKHR swapchain;
	VkImage images[WORLDR_MAX_IMAGES];
	uint32_t image_count;
	uint32_t width;
	uint32_t height;
	char device_name[256];
	int render_major, render_minor, has_render_node;
	VkFormat format;
	VkBool32 sample_shading;
	int dmabuf,dmabuf_formats;
	VkDisplayKHR acquired;
	worldr_scene *scene;
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

static int memory_allow(worldr_vk *vk,VkDeviceSize bytes,char *err,int errlen)
{
 if(bytes>vk->memory_budget || vk->memory_used>vk->memory_budget-bytes){
  char message[192];snprintf(message,sizeof(message),"renderer memory budget exceeded: used=%llu request=%llu limit=%llu",(unsigned long long)vk->memory_used,(unsigned long long)bytes,(unsigned long long)vk->memory_budget);
  seterr(err,errlen,message,VK_ERROR_OUT_OF_DEVICE_MEMORY);return 0;
 }
 return 1;
}
static void memory_added(worldr_vk *vk,VkDeviceSize bytes,int image)
{
 vk->memory_used+=bytes;if(vk->memory_used>vk->memory_peak)vk->memory_peak=vk->memory_used;
 if(image)vk->memory_images++;else vk->memory_buffers++;
}
static void memory_removed(worldr_vk *vk,VkDeviceSize bytes,int image)
{
 vk->memory_used-=bytes;if(image)vk->memory_images--;else vk->memory_buffers--;
}
int worldr_vk_memory_budget(worldr_vk *vk,uint64_t bytes,char *err,int errlen)
{
 if(!vk){seterr(err,errlen,"Vulkan session closed",VK_SUCCESS);return -1;}
 if(!bytes)bytes=WORLDR_MEMORY_BUDGET;
 if(bytes<vk->memory_used){seterr(err,errlen,"memory budget is below current renderer allocations",VK_ERROR_OUT_OF_DEVICE_MEMORY);return -1;}
 vk->memory_budget=bytes;return 0;
}
void worldr_vk_memory_usage(worldr_vk *vk,worldr_vk_memory_stats *stats)
{
 if(!stats)return;
 memset(stats,0,sizeof(*stats));
 if(!vk)return;
 *stats=(worldr_vk_memory_stats){vk->memory_used,vk->memory_peak,vk->memory_budget,vk->memory_images,vk->memory_buffers};
}
static int valid_extent(worldr_vk *vk,uint32_t w,uint32_t h,char *err,int errlen)
{
 VkPhysicalDeviceProperties properties;vkGetPhysicalDeviceProperties(vk->phys,&properties);
 if(!w||!h||w>7680||h>4320||w>properties.limits.maxImageDimension2D||h>properties.limits.maxImageDimension2D||w>properties.limits.maxFramebufferWidth||h>properties.limits.maxFramebufferHeight){seterr(err,errlen,"Vulkan target exceeds supported extent",VK_SUCCESS);return 0;}
 return 1;
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
	const char *exts_wayland[] = {VK_KHR_SURFACE_EXTENSION_NAME, VK_KHR_WAYLAND_SURFACE_EXTENSION_NAME};
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

	if (mode == WORLDR_VK_WAYLAND) {
		ci.enabledExtensionCount = 2;
		ci.ppEnabledExtensionNames = exts_wayland;
	}

	VkResult r = vkCreateInstance(&ci, NULL, out);
	if (r != VK_SUCCESS) {
		seterr(err, errlen, "vkCreateInstance failed — is libvulkan.so.1 / an ICD installed?", r);
		return -1;
	}
	return 0;
}

static int pick_phys(VkInstance inst, VkSurfaceKHR surface, VkPhysicalDevice *out, uint32_t *qfamily, char *err, int errlen)
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
			VkBool32 present = VK_TRUE;
			if (surface && vkGetPhysicalDeviceSurfaceSupportKHR(devs[i], f, surface, &present) != VK_SUCCESS) present = VK_FALSE;
			if ((qs[f].queueFlags & VK_QUEUE_GRAPHICS_BIT) && present) {
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

	const char *swapchain = VK_KHR_SWAPCHAIN_EXTENSION_NAME;
	if (need_swapchain && !has_dev_ext(vk->phys, swapchain)) {
		seterr(err, errlen, "device lacks VK_KHR_swapchain", VK_ERROR_EXTENSION_NOT_PRESENT);
		return -1;
	}
	VkPhysicalDeviceFeatures features;
	vkGetPhysicalDeviceFeatures(vk->phys,&features);
	VkPhysicalDeviceFeatures enabled={.sampleRateShading=features.sampleRateShading};
	vk->sample_shading=enabled.sampleRateShading;
	const char *extensions[5];uint32_t extension_count=0;
	if(need_swapchain)extensions[extension_count++]=swapchain;
	const char *sharing[]={VK_KHR_EXTERNAL_MEMORY_FD_EXTENSION_NAME,VK_EXT_EXTERNAL_MEMORY_DMA_BUF_EXTENSION_NAME,VK_EXT_IMAGE_DRM_FORMAT_MODIFIER_EXTENSION_NAME,VK_EXT_QUEUE_FAMILY_FOREIGN_EXTENSION_NAME};
	vk->dmabuf=1;
	vk->dmabuf_formats=-1;
	for(unsigned i=0;i<4;i++)if(!has_dev_ext(vk->phys,sharing[i]))vk->dmabuf=0;
	if(vk->dmabuf)for(unsigned i=0;i<4;i++)extensions[extension_count++]=sharing[i];
	VkDeviceCreateInfo dci = {.sType=VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO,
		.pEnabledFeatures=&enabled,
		.queueCreateInfoCount=1,.pQueueCreateInfos=&qci,
		.enabledExtensionCount=extension_count,
		.ppEnabledExtensionNames=extensions};

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
			/* A managed DRM connector binds one specific display. A free plane
			 * on a sibling output must not replace that explicit selection. */
			if (vk->acquired && sup[s] != vk->acquired) continue;
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
	if (r != VK_SUCCESS) { seterr(err, errlen, "surface capabilities", r); return -1; }
	if (!(caps.supportedUsageFlags & VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT)) {
		seterr(err, errlen, "surface cannot render color attachments", VK_SUCCESS); return -1;
	}
	if (caps.currentExtent.width != UINT32_MAX) {
		vk->width = caps.currentExtent.width; vk->height = caps.currentExtent.height;
	} else {
		if (vk->width < caps.minImageExtent.width) vk->width = caps.minImageExtent.width;
		if (vk->width > caps.maxImageExtent.width) vk->width = caps.maxImageExtent.width;
		if (vk->height < caps.minImageExtent.height) vk->height = caps.minImageExtent.height;
		if (vk->height > caps.maxImageExtent.height) vk->height = caps.maxImageExtent.height;
	}
	if (!vk->width || !vk->height) { seterr(err, errlen, "surface has zero extent", VK_NOT_READY); return -3; }
	if(!valid_extent(vk,vk->width,vk->height,err,errlen))return -1;
	uint32_t nf = 0;
	r = vkGetPhysicalDeviceSurfaceFormatsKHR(vk->phys, vk->surface, &nf, NULL);
	if (r != VK_SUCCESS || !nf) { seterr(err, errlen, "surface has no formats", r); return -1; }
	VkSurfaceFormatKHR *fmts = calloc(nf, sizeof(*fmts));
	if (!fmts) { seterr(err, errlen, "oom", VK_SUCCESS); return -1; }
	r = vkGetPhysicalDeviceSurfaceFormatsKHR(vk->phys, vk->surface, &nf, fmts);
	if (r != VK_SUCCESS) { free(fmts); seterr(err, errlen, "surface formats", r); return -1; }
	VkSurfaceFormatKHR chosen = { .format=VK_FORMAT_UNDEFINED };
	/* Output is SDR sRGB. Do not silently present its pixels in an unrelated
	 * HDR/wide-gamut color space merely because that pair was listed first.
	 * https://docs.vulkan.org/refpages/latest/refpages/source/VkColorSpaceKHR.html */
	const VkFormat preferred[]={VK_FORMAT_B8G8R8A8_UNORM,VK_FORMAT_R8G8B8A8_UNORM,VK_FORMAT_B8G8R8A8_SRGB,VK_FORMAT_R8G8B8A8_SRGB};
	if(nf==1&&fmts[0].format==VK_FORMAT_UNDEFINED&&fmts[0].colorSpace==VK_COLOR_SPACE_SRGB_NONLINEAR_KHR){chosen=fmts[0];chosen.format=preferred[0];}
	for(unsigned p=0;p<4&&chosen.format==VK_FORMAT_UNDEFINED;p++)for(uint32_t i=0;i<nf;i++){
		if(fmts[i].format==preferred[p]&&fmts[i].colorSpace==VK_COLOR_SPACE_SRGB_NONLINEAR_KHR){chosen=fmts[i];break;}
	}
	free(fmts);
	if(chosen.format==VK_FORMAT_UNDEFINED){seterr(err,errlen,"surface has no supported SDR sRGB output format",VK_ERROR_FORMAT_NOT_SUPPORTED);return -1;}
	vk->format = chosen.format;
	uint32_t count = caps.minImageCount < 2 ? 2 : caps.minImageCount;
	if (caps.maxImageCount && count > caps.maxImageCount) count = caps.maxImageCount;
	VkSwapchainCreateInfoKHR ci = {0};
	ci.sType = VK_STRUCTURE_TYPE_SWAPCHAIN_CREATE_INFO_KHR;
	ci.surface = vk->surface; ci.minImageCount = count;
	ci.imageFormat = chosen.format; ci.imageColorSpace = chosen.colorSpace;
	ci.imageExtent = (VkExtent2D){vk->width, vk->height};
	ci.imageArrayLayers = 1;
	ci.imageUsage = VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT | (caps.supportedUsageFlags & VK_IMAGE_USAGE_TRANSFER_DST_BIT);
	ci.imageSharingMode = VK_SHARING_MODE_EXCLUSIVE;
	ci.preTransform = caps.currentTransform;
	for (uint32_t bit = 1; bit <= VK_COMPOSITE_ALPHA_INHERIT_BIT_KHR; bit <<= 1) {
		if (caps.supportedCompositeAlpha & bit) { ci.compositeAlpha = bit; break; }
	}
	ci.presentMode = VK_PRESENT_MODE_FIFO_KHR; ci.clipped = VK_TRUE;
	r = vkCreateSwapchainKHR(vk->device, &ci, NULL, &vk->swapchain);
	if (r != VK_SUCCESS) { seterr(err, errlen, "vkCreateSwapchainKHR", r); return -1; }
	r = vkGetSwapchainImagesKHR(vk->device, vk->swapchain, &vk->image_count, NULL);
	if (r != VK_SUCCESS || vk->image_count > WORLDR_MAX_IMAGES) {
		seterr(err, errlen, "unsupported swapchain image count", r); return -1;
	}
	r = vkGetSwapchainImagesKHR(vk->device, vk->swapchain, &vk->image_count, vk->images);
	if (r != VK_SUCCESS) { seterr(err, errlen, "vkGetSwapchainImagesKHR", r); return -1; }
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

static int vk_create_ex(int mode, uint32_t prefer_w, uint32_t prefer_h, int drm_fd, uint32_t connector_id, void *wl_display, void *wl_surface,
			worldr_vk **out, char *err, int errlen)
{
	worldr_vk *vk = (worldr_vk *)calloc(1, sizeof(*vk));
	if (!vk) {
		seterr(err, errlen, "oom", VK_SUCCESS);
		return -1;
	}
	vk->mode = mode;
	vk->memory_budget=WORLDR_MEMORY_BUDGET;
	vk->width = prefer_w;
	vk->height = prefer_h;
	int acquire = mode == WORLDR_VK_DISPLAY && drm_fd >= 0 && connector_id != 0;
	if (create_instance(mode, acquire, &vk->instance, err, errlen) != 0) {
		free(vk);
		return -1;
	}
	if (mode == WORLDR_VK_WAYLAND) {
		VkWaylandSurfaceCreateInfoKHR ci = {0};
		ci.sType = VK_STRUCTURE_TYPE_WAYLAND_SURFACE_CREATE_INFO_KHR;
		ci.display = wl_display; ci.surface = wl_surface;
		VkResult r = vkCreateWaylandSurfaceKHR(vk->instance, &ci, NULL, &vk->surface);
		if (r != VK_SUCCESS) { seterr(err, errlen, "vkCreateWaylandSurfaceKHR", r); worldr_vk_destroy(vk); return -1; }
	}
	if (pick_phys(vk->instance, vk->surface, &vk->phys, &vk->queue_family, err, errlen) != 0) {
		worldr_vk_destroy(vk);
		return -1;
	}
	VkPhysicalDeviceProperties props;
	vkGetPhysicalDeviceProperties(vk->phys, &props);
	snprintf(vk->device_name, sizeof(vk->device_name), "%s", props.deviceName);
#ifdef VK_EXT_physical_device_drm
	if (has_dev_ext(vk->phys, VK_EXT_PHYSICAL_DEVICE_DRM_EXTENSION_NAME)) {
		VkPhysicalDeviceDrmPropertiesEXT drm = {
			.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_DRM_PROPERTIES_EXT};
		VkPhysicalDeviceProperties2 properties = {
			.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_PROPERTIES_2,
			.pNext = &drm};
		vkGetPhysicalDeviceProperties2(vk->phys, &properties);
		if (drm.hasRender && drm.renderMajor >= 0 && drm.renderMinor >= 0) {
			vk->render_major = (int)drm.renderMajor;
			vk->render_minor = (int)drm.renderMinor;
			vk->has_render_node = 1;
		}
	}
#endif

	if (acquire && acquire_drm_display(vk, drm_fd, connector_id, err, errlen) != 0) {
		vkDestroyInstance(vk->instance, NULL);
		free(vk);
		return -1;
	}

	if (create_device(vk, mode != WORLDR_VK_HEADLESS, err, errlen) != 0) {
		worldr_vk_destroy(vk);
		return -1;
	}
	if (mode != WORLDR_VK_HEADLESS) {
		if ((mode == WORLDR_VK_DISPLAY && create_display_surface(vk, prefer_w, prefer_h, err, errlen) != 0) ||
		    create_swapchain(vk, err, errlen) != 0) {
			worldr_vk_destroy(vk);
			return -1;
		}
	} else {
		if (!vk->width) vk->width=64;
		if (!vk->height) vk->height=64;
		vk->format=VK_FORMAT_B8G8R8A8_UNORM;
		if(!valid_extent(vk,vk->width,vk->height,err,errlen)){worldr_vk_destroy(vk);return -1;}
	}
	*out = vk;
	return 0;
}

int worldr_vk_create(int mode, uint32_t prefer_w, uint32_t prefer_h, worldr_vk **out, char *err, int errlen)
{
	return vk_create_ex(mode, prefer_w, prefer_h, -1, 0, NULL, NULL, out, err, errlen);
}

int worldr_vk_create_on_drm(int drm_fd, uint32_t connector_id, uint32_t prefer_w, uint32_t prefer_h,
			    worldr_vk **out, char *err, int errlen)
{
	if (drm_fd < 0 || connector_id == 0) {
		seterr(err, errlen, "VK_EXT_acquire_drm_display needs a master DRM fd and connector", VK_SUCCESS);
		return -1;
	}
	return vk_create_ex(WORLDR_VK_DISPLAY, prefer_w, prefer_h, drm_fd, connector_id, NULL, NULL, out, err, errlen);
}

int worldr_vk_create_wayland(void *display, void *surface, uint32_t w, uint32_t h,
	worldr_vk **out, char *err, int errlen)
{
	if (!display || !surface || !w || !h) { seterr(err, errlen, "invalid Wayland surface", VK_SUCCESS); return -1; }
	return vk_create_ex(WORLDR_VK_WAYLAND, w, h, -1, 0, display, surface, out, err, errlen);
}

int worldr_vk_resize(worldr_vk *vk,uint32_t w,uint32_t h,char *err,int errlen)
{
 if(!vk||!vk->device){seterr(err,errlen,"Vulkan session closed",VK_SUCCESS);return -1;}
 if(!w||!h){seterr(err,errlen,"surface has zero extent",VK_NOT_READY);return -3;}
 if(!valid_extent(vk,w,h,err,errlen))return -1;
 if(vk->mode==WORLDR_VK_HEADLESS&&w==vk->width&&h==vk->height&&!vk->needs_recovery)return 0;
 // Headless work is completely covered by the finite scene fence. WSI still
 // needs presentation completion before releasing its semaphores/swapchain.
 if(vk->mode!=WORLDR_VK_HEADLESS){VkResult r=vkDeviceWaitIdle(vk->device);if(r!=VK_SUCCESS){seterr(err,errlen,"wait before resize",r);return -1;}}
 if(worldr_vk_scene_resize_prepare(vk,err,errlen))return -1;
 uint32_t old_width=vk->width,old_height=vk->height;
 if(vk->swapchain){vkDestroySwapchainKHR(vk->device,vk->swapchain,NULL);vk->swapchain=VK_NULL_HANDLE;}
 vk->width=w;vk->height=h;
 if(vk->mode!=WORLDR_VK_HEADLESS){int result=create_swapchain(vk,err,errlen);if(result){vk->needs_recovery=1;scene_mark_failed(vk);return result;}}
 if(worldr_vk_scene_resize_finish(vk,err,errlen)){
  vk->needs_recovery=1;
  if(vk->mode==WORLDR_VK_HEADLESS){
   char rollback[256];vk->width=old_width;vk->height=old_height;
   if(!worldr_vk_scene_resize_finish(vk,rollback,sizeof(rollback)))vk->needs_recovery=0;
  }
  return -1;
 }
 vk->needs_recovery=0;return 0;
}

int worldr_vk_recover(worldr_vk *vk,char *err,int errlen)
{
 if(!vk||!vk->instance||!vk->phys){seterr(err,errlen,"Vulkan session closed",VK_SUCCESS);return -1;}
 if(vk->device){
  VkResult idle=vkDeviceWaitIdle(vk->device);
  if(idle!=VK_SUCCESS&&idle!=VK_ERROR_DEVICE_LOST){seterr(err,errlen,"wait before device recovery",idle);return -1;}
  scene_destroy(vk);
  if(vk->swapchain)vkDestroySwapchainKHR(vk->device,vk->swapchain,NULL);
  vk->swapchain=VK_NULL_HANDLE;vkDestroyDevice(vk->device,NULL);vk->device=VK_NULL_HANDLE;vk->queue=VK_NULL_HANDLE;
 }
 vk->needs_recovery=1;
 if(create_device(vk,vk->mode!=WORLDR_VK_HEADLESS,err,errlen))return -1;
 if(vk->mode!=WORLDR_VK_HEADLESS){int result=create_swapchain(vk,err,errlen);if(result)return result;}
 vk->needs_recovery=0;return 0;
}

void worldr_vk_destroy(worldr_vk *vk)
{
	if (!vk) {
		return;
	}
	if (vk->device) {
		/* Drain both rendering and presentation before destroying WSI resources. */
		vkDeviceWaitIdle(vk->device);
	}
	scene_destroy(vk);
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

int worldr_vk_render_node(const worldr_vk *vk, uint32_t *major,
                          uint32_t *minor)
{
	if (!vk || !vk->has_render_node || !major || !minor) {
		return -1;
	}
	*major = (uint32_t)vk->render_major;
	*minor = (uint32_t)vk->render_minor;
	return 0;
}

uint32_t worldr_vk_width(const worldr_vk *vk)
{
	return vk ? vk->width : 0;
}

uint32_t worldr_vk_height(const worldr_vk *vk)
{
	return vk ? vk->height : 0;
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

int worldr_vk_is_display(const worldr_vk *vk)
{
	return vk && vk->mode != WORLDR_VK_HEADLESS && vk->device;
}

#include "vk_scene.h"
