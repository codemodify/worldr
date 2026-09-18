/* Programmable triangle renderer, included after vk_session.c's ABI helpers.
 * Each call completes with a finite fence wait; resources persist between frames.
 */
#include <stddef.h>
#include <math.h>
#include "vk_frame.h"
#include "scene_shaders.h"

typedef struct {
	VkImage image;
	VkDeviceMemory memory;
	VkImageView view;
	VkDeviceSize allocation;
} scene_image;

typedef struct {
	VkBuffer buffer;
	VkDeviceMemory memory;
	void *mapped;
	VkDeviceSize capacity,allocation;
} scene_buffer;

typedef struct scene_geometry {
	uint64_t id;
	scene_buffer storage;
	VkDeviceSize index_offset;
	uint32_t index_count;
	struct scene_geometry *next;
} scene_geometry;

typedef struct scene_texture {
	uint64_t id;
	uint32_t width, height;
	scene_image image;
	VkDescriptorPool pool;
	VkDescriptorSet descriptor;
	struct scene_texture *next;
} scene_texture;

typedef struct {
	float projection[16], model[16], eye[4], light[4], color[4], wire[4], params[4];
	float material[4], rim[4];
	float glow[4];
	float optical[4];
	float point_position[4][4], point_color[4][4];
	float shadow_projection[16], shadow_params[4];
} scene_uniform;

/* Frame uniforms are copied as one contiguous tail; keep C and std140 aligned. */
_Static_assert(sizeof(scene_uniform)==480,"scene shader uniform size changed");
_Static_assert(offsetof(scene_uniform,material)==208 && offsetof(scene_uniform,rim)==224,"scene material uniform offsets changed");
_Static_assert(offsetof(worldr_frame_draw,material)-offsetof(worldr_frame_draw,projection)==208 &&
	offsetof(worldr_frame_draw,rim)-offsetof(worldr_frame_draw,projection)==224,"frame material uniform offsets changed");
_Static_assert(sizeof(worldr_frame_draw)-offsetof(worldr_frame_draw,projection)==sizeof(scene_uniform),"frame uniform tail size mismatch");
_Static_assert(offsetof(scene_uniform,optical)==256 && offsetof(scene_uniform,point_position)==272 &&
	offsetof(scene_uniform,point_color)==336,"scene optical/light uniform offsets changed");
_Static_assert(offsetof(worldr_frame_draw,optical)-offsetof(worldr_frame_draw,projection)==256 &&
	offsetof(worldr_frame_draw,point_position)-offsetof(worldr_frame_draw,projection)==272 &&
	offsetof(worldr_frame_draw,point_color)-offsetof(worldr_frame_draw,projection)==336,"frame optical/light uniform offsets changed");
_Static_assert(offsetof(scene_uniform,shadow_projection)==400 && offsetof(scene_uniform,shadow_params)==464,"shadow uniform offsets changed");
_Static_assert(offsetof(scene_uniform,glow)==240 &&
	offsetof(worldr_frame_draw,glow)-offsetof(worldr_frame_draw,projection)==240,"scene glow uniform offset changed");

typedef struct scene_transparency scene_transparency;
static void scene_transparency_release(worldr_vk *vk);
typedef struct scene_shadow scene_shadow;
static void scene_shadow_release(worldr_vk *vk);
typedef struct scene_output scene_output;
static void scene_output_release(worldr_vk *vk);
static int scene_output_prepare(worldr_vk *vk, char *err, int errlen);
static void scene_output_render(worldr_vk *vk, uint32_t index, const float *transform);
typedef struct scene_glow scene_glow;
static void scene_glow_release(worldr_vk *vk);

struct worldr_scene {
	VkCommandPool pool;
	VkCommandBuffer cmd;
	VkFence fence;
	VkSemaphore acquired;
	VkSemaphore complete[WORLDR_MAX_IMAGES];
	VkRenderPass pass;
	VkPipelineLayout layout;
	VkPipeline overlay_pipeline, mesh_pipeline, surface_pipeline, mesh_readonly_pipeline, image_pipeline, translucent_pipeline;
	VkPipelineLayout mesh_layout, surface_layout;
	VkDescriptorSetLayout mesh_descriptor_layout;
	VkDescriptorPool mesh_descriptor_pool;
	VkDescriptorSet mesh_descriptor;
	scene_buffer uniforms;
	VkDeviceSize uniform_stride;
	scene_geometry *geometry;
	scene_texture *textures;
	VkDescriptorSetLayout descriptor_layout;
	VkDescriptorPool descriptor_pool;
	VkDescriptorSet descriptor;
	VkSampler sampler;
	VkFramebuffer frames[WORLDR_MAX_IMAGES];
	VkImageView views[WORLDR_MAX_IMAGES];
	scene_image target, depth, multisample, atlas, pending_atlas;
	scene_image pending_import,pending_export;
	scene_buffer vertices, readback, atlas_upload;
	VkFormat depth_format, color_format;
	int linear_color;
	scene_output *output;
	scene_shadow *shadow;
	scene_transparency *transparency;
	VkSampleCountFlagBits samples;
	int atlas_ready;
	int failed;
	scene_glow *glow;
};

static void scene_drop_image(worldr_vk *vk, scene_image *image)
{
	if (image->view) vkDestroyImageView(vk->device, image->view, NULL);
	if (image->image) vkDestroyImage(vk->device, image->image, NULL);
	if (image->memory) {vkFreeMemory(vk->device, image->memory, NULL);memory_removed(vk,image->allocation,1);}
	memset(image, 0, sizeof(*image));
}

static void scene_drop_buffer(worldr_vk *vk, scene_buffer *buffer)
{
	if (buffer->mapped) vkUnmapMemory(vk->device, buffer->memory);
	if (buffer->buffer) vkDestroyBuffer(vk->device, buffer->buffer, NULL);
	if (buffer->memory) {vkFreeMemory(vk->device, buffer->memory, NULL);memory_removed(vk,buffer->allocation,0);}
	memset(buffer, 0, sizeof(*buffer));
}

static void scene_drop_texture(worldr_vk *vk, scene_texture *texture)
{
	if (texture->pool) vkDestroyDescriptorPool(vk->device,texture->pool,NULL);
	scene_drop_image(vk,&texture->image);
	free(texture);
}

static void scene_release_targets(worldr_vk *vk)
{
	worldr_scene *s = vk->scene;
	if (!s) return;
	scene_glow_release(vk);
	scene_shadow_release(vk);
	scene_transparency_release(vk);
	scene_output_release(vk);
	for (unsigned i=0; i<WORLDR_MAX_IMAGES; i++) {
		if (s->frames[i]) vkDestroyFramebuffer(vk->device,s->frames[i],NULL);
		if (s->views[i]) vkDestroyImageView(vk->device,s->views[i],NULL);
		if (s->complete[i]) vkDestroySemaphore(vk->device,s->complete[i],NULL);
		s->frames[i]=VK_NULL_HANDLE; s->views[i]=VK_NULL_HANDLE; s->complete[i]=VK_NULL_HANDLE;
	}
	if (s->overlay_pipeline) vkDestroyPipeline(vk->device,s->overlay_pipeline,NULL);
	if (s->mesh_pipeline) vkDestroyPipeline(vk->device,s->mesh_pipeline,NULL);
	if (s->mesh_readonly_pipeline) vkDestroyPipeline(vk->device,s->mesh_readonly_pipeline,NULL);
	if (s->surface_pipeline) vkDestroyPipeline(vk->device,s->surface_pipeline,NULL);
	if (s->image_pipeline) vkDestroyPipeline(vk->device,s->image_pipeline,NULL);
	if (s->translucent_pipeline) vkDestroyPipeline(vk->device,s->translucent_pipeline,NULL);
	if (s->pass) vkDestroyRenderPass(vk->device,s->pass,NULL);
	s->overlay_pipeline=s->mesh_pipeline=s->surface_pipeline=s->mesh_readonly_pipeline=s->image_pipeline=s->translucent_pipeline=VK_NULL_HANDLE;
	s->pass=VK_NULL_HANDLE;
	scene_drop_image(vk,&s->target);
	scene_drop_image(vk,&s->depth);
	scene_drop_image(vk,&s->multisample);
}

static void scene_destroy(worldr_vk *vk)
{
	worldr_scene *s=vk->scene;
	if (!s) return;
	scene_release_targets(vk);
	if (s->layout) vkDestroyPipelineLayout(vk->device,s->layout,NULL);
	if (s->mesh_layout) vkDestroyPipelineLayout(vk->device,s->mesh_layout,NULL);
	if (s->surface_layout) vkDestroyPipelineLayout(vk->device,s->surface_layout,NULL);
	while (s->textures) {
		scene_texture *texture=s->textures;
		s->textures=texture->next;
		scene_drop_texture(vk,texture);
	}
	if (s->descriptor_pool) vkDestroyDescriptorPool(vk->device,s->descriptor_pool,NULL);
	if (s->descriptor_layout) vkDestroyDescriptorSetLayout(vk->device,s->descriptor_layout,NULL);
	if (s->mesh_descriptor_pool) vkDestroyDescriptorPool(vk->device,s->mesh_descriptor_pool,NULL);
	if (s->mesh_descriptor_layout) vkDestroyDescriptorSetLayout(vk->device,s->mesh_descriptor_layout,NULL);
	if (s->sampler) vkDestroySampler(vk->device,s->sampler,NULL);
	scene_drop_image(vk,&s->atlas);
	scene_drop_image(vk,&s->pending_atlas);
	scene_drop_image(vk,&s->pending_import);
	scene_drop_image(vk,&s->pending_export);
	scene_drop_buffer(vk,&s->vertices);
	scene_drop_buffer(vk,&s->readback);
	scene_drop_buffer(vk,&s->atlas_upload);
	scene_drop_buffer(vk,&s->uniforms);
	while (s->geometry) {
		scene_geometry *g=s->geometry;
		s->geometry=g->next;
		scene_drop_buffer(vk,&g->storage);
		free(g);
	}
	if (s->acquired) vkDestroySemaphore(vk->device,s->acquired,NULL);
	if (s->fence) vkDestroyFence(vk->device,s->fence,NULL);
	if (s->pool) vkDestroyCommandPool(vk->device,s->pool,NULL);
	free(s); vk->scene=NULL;
}

static int scene_wait(worldr_vk *vk, char *err, int errlen)
{
	worldr_scene *s = vk->scene;
	if (!s || !s->fence) return 0;
	VkResult r = vkWaitForFences(vk->device, 1, &s->fence, VK_TRUE, WORLDR_VK_WAIT_NS);
	if (r != VK_SUCCESS) {
		s->failed = 1;
		seterr(err, errlen, "scene GPU fence wait failed (2s limit)", r);
		return -1;
	}
	return 0;
}

static int scene_buffer_reserve(worldr_vk *vk, scene_buffer *b, VkDeviceSize need,
	VkBufferUsageFlags usage, char *err, int errlen)
{
	if (b->buffer && b->capacity >= need) return 0;
	scene_drop_buffer(vk, b);
	VkDeviceSize capacity = 4096;
	while (capacity < need) capacity *= 2;
	VkBufferCreateInfo bi = { .sType = VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,
		.size = capacity, .usage = usage, .sharingMode = VK_SHARING_MODE_EXCLUSIVE };
	VkResult r = vkCreateBuffer(vk->device, &bi, NULL, &b->buffer);
	if (r != VK_SUCCESS) goto fail;
	VkMemoryRequirements req;
	vkGetBufferMemoryRequirements(vk->device, b->buffer, &req);
	uint32_t type = find_memory(vk->phys, req.memoryTypeBits,
		VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT);
	if (type == UINT32_MAX) { r = VK_ERROR_FEATURE_NOT_PRESENT; goto fail; }
	VkMemoryAllocateInfo ai = { .sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,
		.allocationSize = req.size, .memoryTypeIndex = type };
	if(!memory_allow(vk,req.size,err,errlen)){scene_drop_buffer(vk,b);return -1;}
	r = vkAllocateMemory(vk->device, &ai, NULL, &b->memory);
	if (r != VK_SUCCESS) goto fail;
	b->allocation=req.size;memory_added(vk,req.size,0);
	r = vkBindBufferMemory(vk->device, b->buffer, b->memory, 0);
	if (r != VK_SUCCESS) goto fail;
	r = vkMapMemory(vk->device, b->memory, 0, req.size, 0, &b->mapped);
	if (r != VK_SUCCESS) goto fail;
	b->capacity = capacity;
	return 0;
fail:
	seterr(err, errlen, "scene host buffer allocation failed", r);
	scene_drop_buffer(vk, b);
	return -1;
}

static int scene_view(worldr_vk *vk, VkImage image, VkFormat format,
	VkImageAspectFlags aspect, VkImageView *out, char *err, int errlen)
{
	VkImageViewCreateInfo vi = { .sType = VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO,
		.image = image, .viewType = VK_IMAGE_VIEW_TYPE_2D, .format = format,
		.subresourceRange = { .aspectMask = aspect, .levelCount = 1, .layerCount = 1 } };
	VkResult r = vkCreateImageView(vk->device, &vi, NULL, out);
	if (r != VK_SUCCESS) { seterr(err, errlen, "scene image view failed", r); return -1; }
	return 0;
}

static int scene_make_image_samples(worldr_vk *vk, scene_image *out, uint32_t w, uint32_t h,
	VkFormat format, VkSampleCountFlagBits samples, VkImageUsageFlags usage, VkImageAspectFlags aspect, char *err, int errlen)
{
	VkImageCreateInfo ii = { .sType = VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO,
		.imageType = VK_IMAGE_TYPE_2D, .format = format, .extent = { w, h, 1 },
		.mipLevels = 1, .arrayLayers = 1, .samples = samples,
		.tiling = VK_IMAGE_TILING_OPTIMAL, .usage = usage,
		.sharingMode = VK_SHARING_MODE_EXCLUSIVE, .initialLayout = VK_IMAGE_LAYOUT_UNDEFINED };
	VkResult r = vkCreateImage(vk->device, &ii, NULL, &out->image);
	if (r != VK_SUCCESS) goto fail;
	VkMemoryRequirements req;
	vkGetImageMemoryRequirements(vk->device, out->image, &req);
	uint32_t type = UINT32_MAX;
	if (usage & VK_IMAGE_USAGE_TRANSIENT_ATTACHMENT_BIT)
		type = find_memory(vk->phys, req.memoryTypeBits, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT | VK_MEMORY_PROPERTY_LAZILY_ALLOCATED_BIT);
	if (type == UINT32_MAX) type = find_memory(vk->phys, req.memoryTypeBits, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
	if (type == UINT32_MAX) type = find_memory(vk->phys, req.memoryTypeBits, 0);
	if (type == UINT32_MAX) { r = VK_ERROR_FEATURE_NOT_PRESENT; goto fail; }
	VkMemoryAllocateInfo ai = { .sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,
		.allocationSize = req.size, .memoryTypeIndex = type };
	if(!memory_allow(vk,req.size,err,errlen)){scene_drop_image(vk,out);return -1;}
	r = vkAllocateMemory(vk->device, &ai, NULL, &out->memory);
	if (r != VK_SUCCESS) goto fail;
	out->allocation=req.size;memory_added(vk,req.size,1);
	r = vkBindImageMemory(vk->device, out->image, out->memory, 0);
	if (r != VK_SUCCESS) goto fail;
	if (scene_view(vk, out->image, format, aspect, &out->view, err, errlen) != 0) {
		scene_drop_image(vk, out);
		return -1;
	}
	return 0;
fail:
	seterr(err, errlen, "scene GPU image allocation failed", r);
	scene_drop_image(vk, out);
	return -1;
}

static int scene_make_image(worldr_vk *vk, scene_image *out, uint32_t w, uint32_t h,
	VkFormat format, VkImageUsageFlags usage, VkImageAspectFlags aspect, char *err, int errlen)
{
	return scene_make_image_samples(vk,out,w,h,format,VK_SAMPLE_COUNT_1_BIT,usage,aspect,err,errlen);
}

static int scene_pipeline(worldr_vk *vk, int kind, char *err, int errlen)
{
	worldr_scene *s = vk->scene;
	int image=kind==5, translucent=kind==6, readonly=kind==4, mesh=kind==2||readonly, surface=kind==3||image||translucent, overlay=kind==0;
	VkShaderModule vert = VK_NULL_HANDLE, frag = VK_NULL_HANDLE;
	VkShaderModuleCreateInfo shader = { .sType = VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,
		.codeSize = surface ? sizeof(worldr_scene_surface_vert) : mesh ? sizeof(worldr_scene_mesh_vert) : sizeof(worldr_scene_vert),
		.pCode = surface ? worldr_scene_surface_vert : mesh ? worldr_scene_mesh_vert : worldr_scene_vert };
	VkResult r = vkCreateShaderModule(vk->device, &shader, NULL, &vert);
	if (r != VK_SUCCESS) goto done;
	shader.codeSize = translucent ? sizeof(worldr_scene_translucent_frag) : image ? sizeof(worldr_scene_image_frag) : surface ? sizeof(worldr_scene_surface_frag) : mesh ? sizeof(worldr_scene_mesh_frag) : sizeof(worldr_scene_frag);
	shader.pCode = translucent ? worldr_scene_translucent_frag : image ? worldr_scene_image_frag : surface ? worldr_scene_surface_frag : mesh ? worldr_scene_mesh_frag : worldr_scene_frag;
	r = vkCreateShaderModule(vk->device, &shader, NULL, &frag);
	if (r != VK_SUCCESS) goto done;
	VkPipelineShaderStageCreateInfo stages[2] = {
		{ .sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,
		  .stage = VK_SHADER_STAGE_VERTEX_BIT, .module = vert, .pName = "main" },
		{ .sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,
		  .stage = VK_SHADER_STAGE_FRAGMENT_BIT, .module = frag, .pName = "main" }
	};
	uint32_t linear_color=(uint32_t)s->linear_color;
	VkSpecializationMapEntry color_entry={.constantID=0,.offset=0,.size=sizeof(linear_color)};
	VkSpecializationInfo color_mode={.mapEntryCount=1,.pMapEntries=&color_entry,.dataSize=sizeof(linear_color),.pData=&linear_color};
	stages[0].pSpecializationInfo=stages[1].pSpecializationInfo=&color_mode;
	VkVertexInputBindingDescription binding = { .binding = 0,
		.stride = mesh ? sizeof(worldr_mesh_vertex) : sizeof(worldr_scene_vertex), .inputRate = VK_VERTEX_INPUT_RATE_VERTEX };
	VkVertexInputAttributeDescription attrs[4] = {
		{ .location = 0, .binding = 0, .format = VK_FORMAT_R32G32B32_SFLOAT, .offset = offsetof(worldr_scene_vertex, x) },
		{ .location = 1, .binding = 0, .format = VK_FORMAT_R32G32_SFLOAT, .offset = offsetof(worldr_scene_vertex, u) },
		{ .location = 2, .binding = 0, .format = VK_FORMAT_R32G32B32A32_SFLOAT, .offset = offsetof(worldr_scene_vertex, r) }
	};
	if (mesh) {
		attrs[0]=(VkVertexInputAttributeDescription){0,0,VK_FORMAT_R32G32B32_SFLOAT,offsetof(worldr_mesh_vertex,x)};
		attrs[1]=(VkVertexInputAttributeDescription){1,0,VK_FORMAT_R32G32B32_SFLOAT,offsetof(worldr_mesh_vertex,nx)};
		attrs[2]=(VkVertexInputAttributeDescription){2,0,VK_FORMAT_R32G32B32A32_SFLOAT,offsetof(worldr_mesh_vertex,r)};
		attrs[3]=(VkVertexInputAttributeDescription){3,0,VK_FORMAT_R32G32B32_SFLOAT,offsetof(worldr_mesh_vertex,bx)};
	}
	VkPipelineVertexInputStateCreateInfo input = { .sType = VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO,
		.vertexBindingDescriptionCount = surface ? 0 : 1, .pVertexBindingDescriptions = &binding,
		.vertexAttributeDescriptionCount = surface ? 0 : mesh ? 4 : 3, .pVertexAttributeDescriptions = attrs };
	VkPipelineInputAssemblyStateCreateInfo assembly = { .sType = VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO,
		.topology = VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST };
	VkViewport viewport = { .x = 0, .y = 0, .width = (float)vk->width, .height = (float)vk->height,
		.minDepth = 0, .maxDepth = 1 };
	VkRect2D scissor = { .extent = { vk->width, vk->height } };
	VkPipelineViewportStateCreateInfo view = { .sType = VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO,
		.viewportCount = 1, .pViewports = &viewport, .scissorCount = 1, .pScissors = &scissor };
	VkPipelineRasterizationStateCreateInfo raster = { .sType = VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO,
		.polygonMode = VK_POLYGON_MODE_FILL, .cullMode = VK_CULL_MODE_NONE,
		.frontFace = VK_FRONT_FACE_COUNTER_CLOCKWISE, .lineWidth = 1 };
	/* Narrow overlay strokes carry a varying alpha coverage fringe. Evaluate
	 * each covered sample when supported: fragment-rate interpolation can
	 * extrapolate outside these triangles and produced unstable edge coverage
	 * on Intel/Mesa when multiple fringes share a pixel. Mesh and texture
	 * interiors retain fragment-rate shading, including pixel-sharp text.
	 * https://docs.vulkan.org/spec/latest/chapters/primsrast.html#primsrast-sampleshading */
	VkPipelineMultisampleStateCreateInfo samples = { .sType = VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO,
		.rasterizationSamples = s->samples,
		.sampleShadingEnable = overlay && s->samples!=VK_SAMPLE_COUNT_1_BIT && vk->sample_shading,
		.minSampleShading = 1.0f };
	VkPipelineDepthStencilStateCreateInfo depth = { .sType = VK_STRUCTURE_TYPE_PIPELINE_DEPTH_STENCIL_STATE_CREATE_INFO,
		.depthTestEnable = !overlay && !image, .depthWriteEnable = !overlay && !image && !readonly && !translucent, .depthCompareOp = VK_COMPARE_OP_LESS_OR_EQUAL };
	VkPipelineColorBlendAttachmentState blend = { .blendEnable = VK_TRUE,
		.srcColorBlendFactor = VK_BLEND_FACTOR_SRC_ALPHA, .dstColorBlendFactor = VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA,
		.colorBlendOp = VK_BLEND_OP_ADD, .srcAlphaBlendFactor = VK_BLEND_FACTOR_ONE,
		.dstAlphaBlendFactor = VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA, .alphaBlendOp = VK_BLEND_OP_ADD,
		.colorWriteMask = VK_COLOR_COMPONENT_R_BIT | VK_COLOR_COMPONENT_G_BIT | VK_COLOR_COMPONENT_B_BIT | VK_COLOR_COMPONENT_A_BIT };
	if (surface && !image && !translucent) blend.blendEnable=VK_FALSE;
	if (image || translucent) blend.srcColorBlendFactor=VK_BLEND_FACTOR_ONE;
	VkPipelineColorBlendStateCreateInfo blending = { .sType = VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO,
		.attachmentCount = 1, .pAttachments = &blend };
	VkDynamicState dynamics[]={VK_DYNAMIC_STATE_VIEWPORT,VK_DYNAMIC_STATE_SCISSOR};
	VkPipelineDynamicStateCreateInfo dynamic={.sType=VK_STRUCTURE_TYPE_PIPELINE_DYNAMIC_STATE_CREATE_INFO,
		.dynamicStateCount=2,.pDynamicStates=dynamics};
	VkGraphicsPipelineCreateInfo pipeline = { .sType = VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO,
		.stageCount = 2, .pStages = stages, .pVertexInputState = &input, .pInputAssemblyState = &assembly,
		.pViewportState = &view, .pRasterizationState = &raster, .pMultisampleState = &samples,
		.pDepthStencilState = &depth, .pColorBlendState = &blending,
		.pDynamicState = &dynamic, .layout = surface ? s->surface_layout : mesh ? s->mesh_layout : s->layout, .renderPass = s->pass, .subpass = 0 };
	r = vkCreateGraphicsPipelines(vk->device, VK_NULL_HANDLE, 1, &pipeline, NULL,
		translucent ? &s->translucent_pipeline : image ? &s->image_pipeline : readonly ? &s->mesh_readonly_pipeline : surface ? &s->surface_pipeline : mesh ? &s->mesh_pipeline : &s->overlay_pipeline);
done:
	if (vert) vkDestroyShaderModule(vk->device, vert, NULL);
	if (frag) vkDestroyShaderModule(vk->device, frag, NULL);
	if (r != VK_SUCCESS) { seterr(err, errlen, "scene graphics pipeline failed", r); return -1; }
	return 0;
}

static VkSampleCountFlagBits scene_sample_count(worldr_vk *vk,VkFormat depth_format)
{
	/* The overlay's alpha fringes need full sample-rate shading for stable
	 * coverage. Use the proven single-sample path if that feature is absent. */
	if (!vk->sample_shading) return VK_SAMPLE_COUNT_1_BIT;
	VkPhysicalDeviceProperties properties;
	vkGetPhysicalDeviceProperties(vk->phys,&properties);
	VkSampleCountFlags common=properties.limits.framebufferColorSampleCounts & properties.limits.framebufferDepthSampleCounts;
	if (!(common & VK_SAMPLE_COUNT_4_BIT)) return VK_SAMPLE_COUNT_1_BIT;
	/* Framebuffer limits alone are not sufficient: query the exact image
	 * format/usage combinations, including the transient color attachment.
	 * https://docs.vulkan.org/refpages/latest/refpages/source/vkGetPhysicalDeviceImageFormatProperties.html */
	VkImageFormatProperties color,depth;
	if (vkGetPhysicalDeviceImageFormatProperties(vk->phys,vk->scene->color_format,VK_IMAGE_TYPE_2D,VK_IMAGE_TILING_OPTIMAL,
		VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT|VK_IMAGE_USAGE_TRANSIENT_ATTACHMENT_BIT,0,&color)!=VK_SUCCESS ||
		vkGetPhysicalDeviceImageFormatProperties(vk->phys,depth_format,VK_IMAGE_TYPE_2D,VK_IMAGE_TILING_OPTIMAL,
		VK_IMAGE_USAGE_DEPTH_STENCIL_ATTACHMENT_BIT,0,&depth)!=VK_SUCCESS) return VK_SAMPLE_COUNT_1_BIT;
	common &= color.sampleCounts & depth.sampleCounts;
	if (!(common & VK_SAMPLE_COUNT_4_BIT) || vk->width>color.maxExtent.width || vk->height>color.maxExtent.height ||
		vk->width>depth.maxExtent.width || vk->height>depth.maxExtent.height) return VK_SAMPLE_COUNT_1_BIT;
	return VK_SAMPLE_COUNT_4_BIT;
}

uint32_t worldr_vk_scene_samples(worldr_vk *vk)
{
	return vk && vk->scene ? (uint32_t)vk->scene->samples : 0;
}

static int scene_create_targets(worldr_vk *vk,char *err,int errlen)
{
	worldr_scene *s=vk->scene;
	VkResult r;
	s->color_format=s->linear_color?VK_FORMAT_R16G16B16A16_SFLOAT:vk->format;
	VkFormatProperties props;
	vkGetPhysicalDeviceFormatProperties(vk->phys, VK_FORMAT_D32_SFLOAT, &props);
	s->depth_format = props.optimalTilingFeatures & VK_FORMAT_FEATURE_DEPTH_STENCIL_ATTACHMENT_BIT ? VK_FORMAT_D32_SFLOAT : VK_FORMAT_D16_UNORM;
	s->samples=scene_sample_count(vk,s->depth_format);
	int multisampled=s->samples!=VK_SAMPLE_COUNT_1_BIT;
	VkImageLayout resolved_layout=s->linear_color?VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL:(vk->mode!=WORLDR_VK_HEADLESS?VK_IMAGE_LAYOUT_PRESENT_SRC_KHR:VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL);
	VkAttachmentDescription attachments[3] = {
		{ .format = s->color_format, .samples = s->samples,
		  .loadOp = VK_ATTACHMENT_LOAD_OP_CLEAR, .storeOp = VK_ATTACHMENT_STORE_OP_STORE,
		  .stencilLoadOp = VK_ATTACHMENT_LOAD_OP_DONT_CARE, .stencilStoreOp = VK_ATTACHMENT_STORE_OP_DONT_CARE,
		  .initialLayout = VK_IMAGE_LAYOUT_UNDEFINED,
		  .finalLayout = resolved_layout },
		{ .format = s->depth_format, .samples = s->samples,
		  .loadOp = VK_ATTACHMENT_LOAD_OP_CLEAR, .storeOp = VK_ATTACHMENT_STORE_OP_DONT_CARE,
		  .stencilLoadOp = VK_ATTACHMENT_LOAD_OP_DONT_CARE, .stencilStoreOp = VK_ATTACHMENT_STORE_OP_DONT_CARE,
		  .initialLayout = VK_IMAGE_LAYOUT_UNDEFINED, .finalLayout = VK_IMAGE_LAYOUT_DEPTH_STENCIL_ATTACHMENT_OPTIMAL },
		{ .format = s->color_format, .samples = VK_SAMPLE_COUNT_1_BIT,
		  .loadOp = VK_ATTACHMENT_LOAD_OP_DONT_CARE, .storeOp = VK_ATTACHMENT_STORE_OP_STORE,
		  .stencilLoadOp = VK_ATTACHMENT_LOAD_OP_DONT_CARE, .stencilStoreOp = VK_ATTACHMENT_STORE_OP_DONT_CARE,
		  .initialLayout = VK_IMAGE_LAYOUT_UNDEFINED, .finalLayout = resolved_layout }
	};
	if (multisampled) {
		attachments[0].storeOp=VK_ATTACHMENT_STORE_OP_DONT_CARE;
		attachments[0].finalLayout=VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL;
	}
	VkAttachmentReference color = { .attachment = 0, .layout = VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL };
	VkAttachmentReference depth = { .attachment = 1, .layout = VK_IMAGE_LAYOUT_DEPTH_STENCIL_ATTACHMENT_OPTIMAL };
	VkAttachmentReference resolve = { .attachment = 2, .layout = VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL };
	VkSubpassDescription subpass = { .pipelineBindPoint = VK_PIPELINE_BIND_POINT_GRAPHICS,
		.colorAttachmentCount = 1, .pColorAttachments = &color, .pDepthStencilAttachment = &depth,
		.pResolveAttachments = multisampled ? &resolve : NULL };
	VkSubpassDependency dependencies[2] = {
		{ .srcSubpass = VK_SUBPASS_EXTERNAL, .dstSubpass = 0,
		  /* Match the acquire semaphore's COLOR_ATTACHMENT_OUTPUT wait so the
		   * automatic swapchain layout transition cannot run before acquire.
		   * https://docs.vulkan.org/guide/latest/synchronization_examples.html */
		  .srcStageMask = VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT | VK_PIPELINE_STAGE_EARLY_FRAGMENT_TESTS_BIT,
		  .dstStageMask = VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT | VK_PIPELINE_STAGE_EARLY_FRAGMENT_TESTS_BIT,
		  .dstAccessMask = VK_ACCESS_COLOR_ATTACHMENT_READ_BIT | VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT | VK_ACCESS_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT },
		{ .srcSubpass = 0, .dstSubpass = VK_SUBPASS_EXTERNAL,
		  .srcStageMask = VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT,
		  .dstStageMask = s->linear_color ? VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT : (vk->mode != WORLDR_VK_HEADLESS ? VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT : VK_PIPELINE_STAGE_TRANSFER_BIT),
		  .srcAccessMask = VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT,
		  .dstAccessMask = s->linear_color ? VK_ACCESS_SHADER_READ_BIT : (vk->mode != WORLDR_VK_HEADLESS ? 0 : VK_ACCESS_TRANSFER_READ_BIT) }
	};
	VkRenderPassCreateInfo pass = { .sType = VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO,
		.attachmentCount = multisampled ? 3 : 2, .pAttachments = attachments, .subpassCount = 1, .pSubpasses = &subpass,
		.dependencyCount = 2, .pDependencies = dependencies };
	if ((r = vkCreateRenderPass(vk->device, &pass, NULL, &s->pass)) != VK_SUCCESS) goto fail;
	if (scene_make_image_samples(vk, &s->depth, vk->width, vk->height, s->depth_format,s->samples,
		VK_IMAGE_USAGE_DEPTH_STENCIL_ATTACHMENT_BIT, VK_IMAGE_ASPECT_DEPTH_BIT, err, errlen)) goto resource_fail;
	if (multisampled && scene_make_image_samples(vk,&s->multisample,vk->width,vk->height,s->color_format,s->samples,
		VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT|VK_IMAGE_USAGE_TRANSIENT_ATTACHMENT_BIT,VK_IMAGE_ASPECT_COLOR_BIT,err,errlen)) goto resource_fail;
	if (vk->mode == WORLDR_VK_HEADLESS || s->linear_color) {
		if (scene_make_image(vk, &s->target, vk->width, vk->height, s->color_format,
			VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT | VK_IMAGE_USAGE_TRANSFER_SRC_BIT | VK_IMAGE_USAGE_SAMPLED_BIT,
			VK_IMAGE_ASPECT_COLOR_BIT, err, errlen)) goto resource_fail;
	}
	if (vk->mode != WORLDR_VK_HEADLESS) {
		VkSemaphoreCreateInfo sem={.sType=VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO};
		for (uint32_t i=0;i<vk->image_count;i++)
			if ((r=vkCreateSemaphore(vk->device,&sem,NULL,&s->complete[i]))!=VK_SUCCESS) goto fail;
	}
	uint32_t count = vk->mode != WORLDR_VK_HEADLESS ? vk->image_count : 1;
	for (uint32_t i = 0; i < count; i++) {
		VkImageView resolved=s->target.view;
		if (vk->mode != WORLDR_VK_HEADLESS) {
			if (scene_view(vk, vk->images[i], vk->format, VK_IMAGE_ASPECT_COLOR_BIT, &s->views[i], err, errlen)) goto resource_fail;
			if (!s->linear_color) resolved=s->views[i];
		}
		VkImageView views[3]={multisampled?s->multisample.view:resolved,s->depth.view,resolved};
		VkFramebufferCreateInfo frame = { .sType = VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO,
			.renderPass = s->pass, .attachmentCount = multisampled ? 3 : 2, .pAttachments = views,
			.width = vk->width, .height = vk->height, .layers = 1 };
		if ((r = vkCreateFramebuffer(vk->device, &frame, NULL, &s->frames[i])) != VK_SUCCESS) goto fail;
	}
	if (scene_pipeline(vk, 0, err, errlen) || scene_pipeline(vk, 2, err, errlen) ||
		scene_pipeline(vk, 3, err, errlen) || scene_pipeline(vk, 4, err, errlen) || scene_pipeline(vk, 5, err, errlen) || scene_pipeline(vk, 6, err, errlen)) goto resource_fail;
	if (s->linear_color && scene_output_prepare(vk,err,errlen)) goto resource_fail;
	return 0;
fail:
	seterr(err,errlen,"scene target creation failed",r);
resource_fail:
	scene_release_targets(vk);
	return -1;
}

static void scene_mark_failed(worldr_vk *vk){if(vk&&vk->scene)vk->scene->failed=1;}

static int scene_init(worldr_vk *vk, char *err, int errlen)
{
	if(vk->needs_recovery){seterr(err,errlen,"Vulkan target requires recovery",VK_SUCCESS);return -1;}
	if (vk->scene) {
		if (vk->scene->failed) { seterr(err, errlen, "scene session failed; reopen Vulkan session", VK_SUCCESS); return -1; }
		return 0;
	}
	worldr_scene *s = calloc(1, sizeof(*s));
	if (!s) { seterr(err, errlen, "scene allocation failed", VK_ERROR_OUT_OF_HOST_MEMORY); return -1; }
	vk->scene = s;
	VkResult r;
	VkCommandPoolCreateInfo pool = { .sType = VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO,
		.flags = VK_COMMAND_POOL_CREATE_RESET_COMMAND_BUFFER_BIT, .queueFamilyIndex = vk->queue_family };
	if ((r = vkCreateCommandPool(vk->device, &pool, NULL, &s->pool)) != VK_SUCCESS) goto fail;
	VkCommandBufferAllocateInfo cmd = { .sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO,
		.commandPool = s->pool, .level = VK_COMMAND_BUFFER_LEVEL_PRIMARY, .commandBufferCount = 1 };
	if ((r = vkAllocateCommandBuffers(vk->device, &cmd, &s->cmd)) != VK_SUCCESS) goto fail;
	VkFenceCreateInfo fence = { .sType = VK_STRUCTURE_TYPE_FENCE_CREATE_INFO, .flags = VK_FENCE_CREATE_SIGNALED_BIT };
	if ((r = vkCreateFence(vk->device, &fence, NULL, &s->fence)) != VK_SUCCESS) goto fail;
	if (vk->mode != WORLDR_VK_HEADLESS) {
		VkSemaphoreCreateInfo sem = { .sType = VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO };
		if ((r = vkCreateSemaphore(vk->device, &sem, NULL, &s->acquired)) != VK_SUCCESS) goto fail;
	}
	VkDescriptorSetLayoutBinding binding = { .binding = 0, .descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,
		.descriptorCount = 1, .stageFlags = VK_SHADER_STAGE_FRAGMENT_BIT };
	VkDescriptorSetLayoutCreateInfo dl = { .sType = VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO,
		.bindingCount = 1, .pBindings = &binding };
	if ((r = vkCreateDescriptorSetLayout(vk->device, &dl, NULL, &s->descriptor_layout)) != VK_SUCCESS) goto fail;
	VkDescriptorPoolSize size = { .type = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, .descriptorCount = 1 };
	VkDescriptorPoolCreateInfo dp = { .sType = VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO,
		.maxSets = 1, .poolSizeCount = 1, .pPoolSizes = &size };
	if ((r = vkCreateDescriptorPool(vk->device, &dp, NULL, &s->descriptor_pool)) != VK_SUCCESS) goto fail;
	VkDescriptorSetAllocateInfo da = { .sType = VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO,
		.descriptorPool = s->descriptor_pool, .descriptorSetCount = 1, .pSetLayouts = &s->descriptor_layout };
	if ((r = vkAllocateDescriptorSets(vk->device, &da, &s->descriptor)) != VK_SUCCESS) goto fail;
	VkSamplerCreateInfo sampler = { .sType = VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO,
		.magFilter = VK_FILTER_LINEAR, .minFilter = VK_FILTER_LINEAR,
		.mipmapMode = VK_SAMPLER_MIPMAP_MODE_NEAREST,
		.addressModeU = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE,
		.addressModeV = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE,
		.addressModeW = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE, .maxLod = 0 };
	if ((r = vkCreateSampler(vk->device, &sampler, NULL, &s->sampler)) != VK_SUCCESS) goto fail;
	VkPushConstantRange push = { .stageFlags = VK_SHADER_STAGE_VERTEX_BIT, .offset = 0, .size = 2 * sizeof(float) };
	VkPipelineLayoutCreateInfo layout = { .sType = VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO,
		.setLayoutCount = 1, .pSetLayouts = &s->descriptor_layout,
		.pushConstantRangeCount = 1, .pPushConstantRanges = &push };
	if ((r = vkCreatePipelineLayout(vk->device, &layout, NULL, &s->layout)) != VK_SUCCESS) goto fail;
	VkDescriptorSetLayoutBinding mb[2]={
		{.binding=0,.descriptorType=VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER_DYNAMIC,.descriptorCount=1,.stageFlags=VK_SHADER_STAGE_VERTEX_BIT|VK_SHADER_STAGE_FRAGMENT_BIT},
		{.binding=1,.descriptorType=VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,.descriptorCount=1,.stageFlags=VK_SHADER_STAGE_FRAGMENT_BIT}};
	VkDescriptorSetLayoutCreateInfo ml={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO,.bindingCount=2,.pBindings=mb};
	if ((r=vkCreateDescriptorSetLayout(vk->device,&ml,NULL,&s->mesh_descriptor_layout))!=VK_SUCCESS) goto fail;
	VkDescriptorPoolSize ms[2]={{VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER_DYNAMIC,1},{VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,1}};
	VkDescriptorPoolCreateInfo mp={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO,.maxSets=1,.poolSizeCount=2,.pPoolSizes=ms};
	if ((r=vkCreateDescriptorPool(vk->device,&mp,NULL,&s->mesh_descriptor_pool))!=VK_SUCCESS) goto fail;
	VkDescriptorSetAllocateInfo ma={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO,.descriptorPool=s->mesh_descriptor_pool,
		.descriptorSetCount=1,.pSetLayouts=&s->mesh_descriptor_layout};
	if ((r=vkAllocateDescriptorSets(vk->device,&ma,&s->mesh_descriptor))!=VK_SUCCESS) goto fail;
	VkPipelineLayoutCreateInfo mli={.sType=VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO,.setLayoutCount=1,.pSetLayouts=&s->mesh_descriptor_layout};
	if ((r=vkCreatePipelineLayout(vk->device,&mli,NULL,&s->mesh_layout))!=VK_SUCCESS) goto fail;
	VkDescriptorSetLayout surface_sets[]={s->mesh_descriptor_layout,s->descriptor_layout};
	VkPipelineLayoutCreateInfo sli={.sType=VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO,
		.setLayoutCount=2,.pSetLayouts=surface_sets};
	if ((r=vkCreatePipelineLayout(vk->device,&sli,NULL,&s->surface_layout))!=VK_SUCCESS) goto fail;
	VkPhysicalDeviceProperties properties;
	vkGetPhysicalDeviceProperties(vk->phys,&properties);
	VkDeviceSize align=properties.limits.minUniformBufferOffsetAlignment;
	if (align<1) align=1;
	s->uniform_stride=(sizeof(scene_uniform)+align-1)/align*align;
	if (scene_create_targets(vk,err,errlen)) goto resource_fail;
	return 0;
fail:
	seterr(err, errlen, "scene resource creation failed", r);
resource_fail:
	scene_destroy(vk);
	return -1;
}

static int scene_begin(worldr_vk *vk, char *err, int errlen)
{
	worldr_scene *s = vk->scene;
	if (scene_wait(vk, err, errlen)) return -1;
	VkResult r = vkResetCommandBuffer(s->cmd, 0);
	if (r != VK_SUCCESS) { seterr(err, errlen, "scene command reset failed", r); return -1; }
	VkCommandBufferBeginInfo begin = { .sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO,
		.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT };
	r = vkBeginCommandBuffer(s->cmd, &begin);
	if (r != VK_SUCCESS) { seterr(err, errlen, "scene command begin failed", r); return -1; }
	return 0;
}

static int scene_submit(worldr_vk *vk, VkSemaphore wait, VkSemaphore signal, char *err, int errlen)
{
	worldr_scene *s = vk->scene;
	VkResult r = vkEndCommandBuffer(s->cmd);
	if (r != VK_SUCCESS) goto fail;
	r = vkResetFences(vk->device, 1, &s->fence);
	if (r != VK_SUCCESS) goto fail;
	VkPipelineStageFlags stage = VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT;
	VkSubmitInfo submit = { .sType = VK_STRUCTURE_TYPE_SUBMIT_INFO,
		.commandBufferCount = 1, .pCommandBuffers = &s->cmd };
	if (wait) { submit.waitSemaphoreCount = 1; submit.pWaitSemaphores = &wait; submit.pWaitDstStageMask = &stage; }
	if (signal) { submit.signalSemaphoreCount = 1; submit.pSignalSemaphores = &signal; }
	r = vkQueueSubmit(vk->queue, 1, &submit, s->fence);
	if (r != VK_SUCCESS) goto fail;
	return scene_wait(vk, err, errlen);
fail:
	s->failed = 1;
	seterr(err, errlen, "scene command submission failed", r);
	return -1;
}

int worldr_vk_scene_atlas(worldr_vk *vk, uint32_t width, uint32_t height,
	const uint8_t *coverage, char *err, int errlen)
{
	if (!vk || !vk->device || !width || !height || !coverage) {
		seterr(err, errlen, "invalid scene atlas", VK_SUCCESS); return -1;
	}
	if (scene_init(vk, err, errlen) || scene_wait(vk, err, errlen)) return -1;
	worldr_scene *s = vk->scene;
	// Keep the live atlas and descriptor valid until replacement upload completes.
	scene_drop_image(vk,&s->pending_atlas);
	if (scene_make_image(vk, &s->pending_atlas, width, height, VK_FORMAT_R8_UNORM,
		VK_IMAGE_USAGE_SAMPLED_BIT | VK_IMAGE_USAGE_TRANSFER_DST_BIT, VK_IMAGE_ASPECT_COLOR_BIT, err, errlen)) return -1;
	VkDeviceSize bytes = (VkDeviceSize)width * height;
	if (scene_buffer_reserve(vk, &s->atlas_upload, bytes, VK_BUFFER_USAGE_TRANSFER_SRC_BIT, err, errlen)) goto upload_fail;
	memcpy(s->atlas_upload.mapped, coverage, (size_t)bytes);
	if (scene_begin(vk, err, errlen)) goto upload_fail;
	barrier(s->cmd, s->pending_atlas.image, VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
		0, VK_ACCESS_TRANSFER_WRITE_BIT, VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);
	VkBufferImageCopy copy = { .imageSubresource = { .aspectMask = VK_IMAGE_ASPECT_COLOR_BIT, .layerCount = 1 },
		.imageExtent = { width, height, 1 } };
	vkCmdCopyBufferToImage(s->cmd, s->atlas_upload.buffer, s->pending_atlas.image, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &copy);
	barrier(s->cmd, s->pending_atlas.image, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
		VK_ACCESS_TRANSFER_WRITE_BIT, VK_ACCESS_SHADER_READ_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT);
	if (scene_submit(vk, VK_NULL_HANDLE, VK_NULL_HANDLE, err, errlen)) return -1; // Failed submission retains potentially in-flight pending storage.
	scene_drop_image(vk,&s->atlas);s->atlas=s->pending_atlas;memset(&s->pending_atlas,0,sizeof(s->pending_atlas));
	VkDescriptorImageInfo image = { .sampler = s->sampler, .imageView = s->atlas.view,
		.imageLayout = VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL };
	VkWriteDescriptorSet write = { .sType = VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,
		.dstSet = s->descriptor, .dstBinding = 0, .descriptorCount = 1,
		.descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, .pImageInfo = &image };
	vkUpdateDescriptorSets(vk->device, 1, &write, 0, NULL);
	VkDescriptorImageInfo fallback_shadow={.sampler=s->sampler,.imageView=s->atlas.view,.imageLayout=VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL};
	VkWriteDescriptorSet fallback_write={.sType=VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,.dstSet=s->mesh_descriptor,.dstBinding=1,
		.descriptorCount=1,.descriptorType=VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,.pImageInfo=&fallback_shadow};
	vkUpdateDescriptorSets(vk->device,1,&fallback_write,0,NULL);
	s->atlas_ready = 1;
	return 0;
upload_fail:
	scene_drop_image(vk,&s->pending_atlas);return -1;
}

int worldr_vk_scene_resize_prepare(worldr_vk *vk, char *err, int errlen)
{
	if (!vk || !vk->scene) return 0;
	if (scene_wait(vk,err,errlen)) return -1;
	scene_release_targets(vk);
	return 0;
}

int worldr_vk_scene_resize_finish(worldr_vk *vk, char *err, int errlen)
{
	if (!vk || !vk->scene) return 0;
	if (scene_create_targets(vk,err,errlen)) { vk->scene->failed=1; return -1; }
	vk->scene->failed=0;
	return 0;
}

static scene_geometry *scene_find_geometry(worldr_vk *vk,uint64_t id)
{
	if (!vk || !vk->scene) return NULL;
	for (scene_geometry *g=vk->scene->geometry;g;g=g->next) if (g->id==id) return g;
	return NULL;
}

int worldr_vk_has_geometry(worldr_vk *vk,uint64_t id) { return scene_find_geometry(vk,id)!=NULL; }

int worldr_vk_upload_geometry(worldr_vk *vk,uint64_t id,const worldr_mesh_vertex *vertices,uint32_t vertex_count,
	const uint32_t *indices,uint32_t index_count,char *err,int errlen)
{
	if (!vk || !vk->device || !id || !vertices || !indices || !vertex_count || !index_count || index_count%3) {
		seterr(err,errlen,"invalid retained geometry",VK_SUCCESS);return -1;
	}
	if (scene_init(vk,err,errlen) || scene_wait(vk,err,errlen)) return -1;
	if (scene_find_geometry(vk,id)) return 0;
	worldr_scene *s=vk->scene;
	VkDeviceSize vertex_bytes=(VkDeviceSize)vertex_count*sizeof(*vertices);
	VkDeviceSize bytes=vertex_bytes+(VkDeviceSize)index_count*sizeof(*indices);
	for (uint32_t i=0;i<index_count;i++) if (indices[i]>=vertex_count) {
		seterr(err,errlen,"retained geometry index out of range",VK_SUCCESS);return -1;
	}
	scene_geometry *g=calloc(1,sizeof(*g));
	if (!g) { seterr(err,errlen,"geometry allocation failed",VK_ERROR_OUT_OF_HOST_MEMORY);return -1; }
	VkBufferCreateInfo bi={.sType=VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,.size=bytes,
		.usage=VK_BUFFER_USAGE_VERTEX_BUFFER_BIT|VK_BUFFER_USAGE_INDEX_BUFFER_BIT|VK_BUFFER_USAGE_TRANSFER_DST_BIT,
		.sharingMode=VK_SHARING_MODE_EXCLUSIVE};
	VkResult r=vkCreateBuffer(vk->device,&bi,NULL,&g->storage.buffer);
	if (r!=VK_SUCCESS) goto fail;
	VkMemoryRequirements req;
	vkGetBufferMemoryRequirements(vk->device,g->storage.buffer,&req);
	uint32_t type=find_memory(vk->phys,req.memoryTypeBits,VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
	if (type==UINT32_MAX) { r=VK_ERROR_FEATURE_NOT_PRESENT;goto fail; }
	VkMemoryAllocateInfo ai={.sType=VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,.allocationSize=req.size,.memoryTypeIndex=type};
	if(!memory_allow(vk,req.size,err,errlen))goto upload_fail;
	if ((r=vkAllocateMemory(vk->device,&ai,NULL,&g->storage.memory))!=VK_SUCCESS) goto fail;
	g->storage.allocation=req.size;memory_added(vk,req.size,0);
	if ((r=vkBindBufferMemory(vk->device,g->storage.buffer,g->storage.memory,0))!=VK_SUCCESS) goto fail;
	g->storage.capacity=bytes;
	if (scene_buffer_reserve(vk,&s->atlas_upload,bytes,VK_BUFFER_USAGE_TRANSFER_SRC_BIT,err,errlen)) goto upload_fail;
	memcpy(s->atlas_upload.mapped,vertices,(size_t)vertex_bytes);
	memcpy((uint8_t *)s->atlas_upload.mapped+vertex_bytes,indices,(size_t)(bytes-vertex_bytes));
	if (scene_begin(vk,err,errlen)) goto upload_fail;
	VkBufferCopy copy={.size=bytes};
	vkCmdCopyBuffer(s->cmd,s->atlas_upload.buffer,g->storage.buffer,1,&copy);
	VkBufferMemoryBarrier barrier={.sType=VK_STRUCTURE_TYPE_BUFFER_MEMORY_BARRIER,
		.srcAccessMask=VK_ACCESS_TRANSFER_WRITE_BIT,.dstAccessMask=VK_ACCESS_VERTEX_ATTRIBUTE_READ_BIT|VK_ACCESS_INDEX_READ_BIT,
		.srcQueueFamilyIndex=VK_QUEUE_FAMILY_IGNORED,.dstQueueFamilyIndex=VK_QUEUE_FAMILY_IGNORED,
		.buffer=g->storage.buffer,.size=bytes};
	vkCmdPipelineBarrier(s->cmd,VK_PIPELINE_STAGE_TRANSFER_BIT,VK_PIPELINE_STAGE_VERTEX_INPUT_BIT,0,0,NULL,1,&barrier,0,NULL);
	/* Retain before submitting: a failed GPU wait must not free in-flight storage. */
	g->id=id;g->index_offset=vertex_bytes;g->index_count=index_count;g->next=s->geometry;s->geometry=g;
	return scene_submit(vk,VK_NULL_HANDLE,VK_NULL_HANDLE,err,errlen);
fail:
	seterr(err,errlen,"retained geometry GPU allocation failed",r);
upload_fail:
	scene_drop_buffer(vk,&g->storage);free(g);return -1;
}

int worldr_vk_release_geometry(worldr_vk *vk,uint64_t id,char *err,int errlen)
{
	if (!vk || !vk->device) { seterr(err,errlen,"Vulkan session closed",VK_SUCCESS);return -1; }
	if (!vk->scene) return 0;
	if (scene_wait(vk,err,errlen)) return -1;
	scene_geometry **link=&vk->scene->geometry;
	while (*link) {
		scene_geometry *g=*link;
		if (g->id==id) { *link=g->next;scene_drop_buffer(vk,&g->storage);free(g);return 0; }
		link=&g->next;
	}
	return 0;
}

static scene_texture **scene_texture_link(worldr_vk *vk,uint64_t id)
{
	scene_texture **link=&vk->scene->textures;
	while (*link && (*link)->id!=id) link=&(*link)->next;
	return link;
}

int worldr_vk_upload_texture(worldr_vk *vk,uint64_t id,uint32_t width,uint32_t height,
	uint32_t x,uint32_t y,uint32_t damage_width,uint32_t damage_height,
	const uint8_t *rgba,char *err,int errlen)
{
	if (!vk || !vk->device || !id || !width || !height || !rgba || !damage_width || !damage_height ||
		x>=width || y>=height || damage_width>width-x || damage_height>height-y) {
		seterr(err,errlen,"invalid content texture update",VK_SUCCESS);return -1;
	}
	VkPhysicalDeviceProperties properties;
	vkGetPhysicalDeviceProperties(vk->phys,&properties);
	if (width>properties.limits.maxImageDimension2D || height>properties.limits.maxImageDimension2D) {
		seterr(err,errlen,"content texture exceeds device image limits",VK_SUCCESS);return -1;
	}
	if (scene_init(vk,err,errlen) || scene_wait(vk,err,errlen)) return -1;
	worldr_scene *s=vk->scene;
	scene_texture **link=scene_texture_link(vk,id), *old=*link, *texture=old;
	int fresh=!old || old->width!=width || old->height!=height;
	VkResult r=VK_SUCCESS;
	if (fresh) {
		if (x || y || damage_width!=width || damage_height!=height) {
			seterr(err,errlen,"new content texture requires a full image",VK_SUCCESS);return -1;
		}
		texture=calloc(1,sizeof(*texture));
		if (!texture) { seterr(err,errlen,"content texture allocation failed",VK_ERROR_OUT_OF_HOST_MEMORY);return -1; }
		texture->id=id;texture->width=width;texture->height=height;
		if (scene_make_image(vk,&texture->image,width,height,VK_FORMAT_R8G8B8A8_UNORM,
			VK_IMAGE_USAGE_SAMPLED_BIT|VK_IMAGE_USAGE_TRANSFER_DST_BIT,VK_IMAGE_ASPECT_COLOR_BIT,err,errlen)) goto fail;
		VkDescriptorPoolSize size={.type=VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,.descriptorCount=1};
		VkDescriptorPoolCreateInfo pool={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO,
			.maxSets=1,.poolSizeCount=1,.pPoolSizes=&size};
		if ((r=vkCreateDescriptorPool(vk->device,&pool,NULL,&texture->pool))!=VK_SUCCESS) goto allocation_fail;
		VkDescriptorSetAllocateInfo descriptor={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO,
			.descriptorPool=texture->pool,.descriptorSetCount=1,.pSetLayouts=&s->descriptor_layout};
		if ((r=vkAllocateDescriptorSets(vk->device,&descriptor,&texture->descriptor))!=VK_SUCCESS) goto allocation_fail;
	}
	VkDeviceSize bytes=(VkDeviceSize)damage_width*damage_height*4;
	if (scene_buffer_reserve(vk,&s->atlas_upload,bytes,VK_BUFFER_USAGE_TRANSFER_SRC_BIT,err,errlen)) goto fail;
	memcpy(s->atlas_upload.mapped,rgba,(size_t)bytes);
	if (scene_begin(vk,err,errlen)) goto fail;
	barrier(s->cmd,texture->image.image,
		fresh?VK_IMAGE_LAYOUT_UNDEFINED:VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
		fresh?0:VK_ACCESS_SHADER_READ_BIT,VK_ACCESS_TRANSFER_WRITE_BIT,
		fresh?VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT:VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT,VK_PIPELINE_STAGE_TRANSFER_BIT);
	VkBufferImageCopy copy={.imageSubresource={.aspectMask=VK_IMAGE_ASPECT_COLOR_BIT,.layerCount=1},
		.imageOffset={(int32_t)x,(int32_t)y,0},.imageExtent={damage_width,damage_height,1}};
	vkCmdCopyBufferToImage(s->cmd,s->atlas_upload.buffer,texture->image.image,VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,1,&copy);
	barrier(s->cmd,texture->image.image,VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
		VK_ACCESS_TRANSFER_WRITE_BIT,VK_ACCESS_SHADER_READ_BIT,VK_PIPELINE_STAGE_TRANSFER_BIT,VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT);
	if (fresh) {
		VkDescriptorImageInfo image={.sampler=s->sampler,.imageView=texture->image.view,
			.imageLayout=VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL};
		VkWriteDescriptorSet write={.sType=VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,.dstSet=texture->descriptor,
			.dstBinding=0,.descriptorCount=1,.descriptorType=VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,.pImageInfo=&image};
		vkUpdateDescriptorSets(vk->device,1,&write,0,NULL);
		/* Prior work has completed. Retain the new image before submission so a
		 * failed GPU wait never destroys an in-flight resource. */
		texture->next=old?old->next:NULL;
		*link=texture;
		if (old) scene_drop_texture(vk,old);
	}
	return scene_submit(vk,VK_NULL_HANDLE,VK_NULL_HANDLE,err,errlen);
allocation_fail:
	seterr(err,errlen,"content texture descriptor allocation failed",r);
fail:
	if (fresh) scene_drop_texture(vk,texture);
	return -1;
}

int worldr_vk_release_texture(worldr_vk *vk,uint64_t id,char *err,int errlen)
{
	if (!vk || !vk->device) { seterr(err,errlen,"Vulkan session closed",VK_SUCCESS);return -1; }
	if (!vk->scene) return 0;
	if (scene_wait(vk,err,errlen)) return -1;
	scene_texture **link=scene_texture_link(vk,id), *texture=*link;
	if (texture) { *link=texture->next;scene_drop_texture(vk,texture); }
	return 0;
}

static VkRect2D scene_scissor(worldr_vk *vk,const float *v)
{
	float left=v[0]>0?v[0]:0,top=v[1]>0?v[1]:0;
	float right=v[0]+v[2],bottom=v[1]+v[3];
	if (right>vk->width) right=(float)vk->width;
	if (bottom>vk->height) bottom=(float)vk->height;
	if (left>vk->width) left=(float)vk->width;
	if (top>vk->height) top=(float)vk->height;
	int32_t x=(int32_t)left,y=(int32_t)top;
	int32_t r=(int32_t)right+(right>(int32_t)right),b=(int32_t)bottom+(bottom>(int32_t)bottom);
	return (VkRect2D){.offset={x,y},.extent={r>x?(uint32_t)(r-x):0,b>y?(uint32_t)(b-y):0}};
}

static void scene_set_viewport(worldr_vk *vk,const float *v)
{
	VkViewport viewport={.x=v[0],.y=v[1],.width=v[2],.height=v[3],.minDepth=0,.maxDepth=1};
	VkRect2D scissor=scene_scissor(vk,v);
	vkCmdSetViewport(vk->scene->cmd,0,1,&viewport);
	vkCmdSetScissor(vk->scene->cmd,0,1,&scissor);
}

#include "vk_glow.h"
#include "vk_output.h"
#include "vk_dmabuf.h"
#include "vk_shadow.h"
#include "vk_transparency.h"

int worldr_vk_render_frame(worldr_vk *vk,int linear_color,const float *output_transform,
	const worldr_scene_vertex *vertices,uint32_t vertex_count,const worldr_frame_draw *draws,
	uint32_t draw_count,const float *clear,uint8_t *out_bgra,char *err,int errlen)
{
	if (!vk || !vk->device || !clear || (vertex_count&&!vertices) || (draw_count&&!draws)) {
		seterr(err,errlen,"invalid scene frame",VK_SUCCESS);return -1;
	}
	if (out_bgra && vk->mode!=WORLDR_VK_HEADLESS) {
		seterr(err,errlen,"swapchain readback unsupported; render an offscreen snapshot",VK_SUCCESS);return -1;
	}
	scene_transparency_scope transparency_scopes[SCENE_TRANSPARENCY_VIEWS];
	int transparency_count=scene_transparency_scopes(draws,draw_count,transparency_scopes,err,errlen);
	if(transparency_count<0)return -1;
	scene_shadow_scope shadow_scopes[SCENE_SHADOW_VIEWS];
	int shadow_count=scene_shadow_scopes(draws,draw_count,shadow_scopes,err,errlen);
	if(shadow_count<0)return -1;
	scene_glow_scope glow_scopes[SCENE_GLOW_VIEWS];
	int glow_count=scene_glow_scopes(draws,draw_count,glow_scopes,err,errlen);
	if (glow_count<0) return -1;
	if (scene_init(vk,err,errlen)) return -1;
	worldr_scene *s=vk->scene;
	if (s->linear_color != !!linear_color) {
		if (scene_wait(vk,err,errlen)) return -1;
		int previous=s->linear_color;
		scene_release_targets(vk);
		s->linear_color=!!linear_color;
		if (scene_create_targets(vk,err,errlen)) {
			/* Preserve the allocation error while restoring the last usable mode. */
			char rollback_error[256];s->linear_color=previous;
			if(scene_create_targets(vk,rollback_error,sizeof(rollback_error)))s->failed=1;
			return -1;
		}
	}
	if (!s->atlas_ready) {
		const uint8_t white=255;
		if (worldr_vk_scene_atlas(vk,1,1,&white,err,errlen)) return -1;
	}
	if (scene_wait(vk,err,errlen)) return -1;
	if (scene_glow_prepare(vk,(unsigned)glow_count,err,errlen)) return -1;
	if (vertex_count) {
		VkDeviceSize bytes=(VkDeviceSize)vertex_count*sizeof(*vertices);
		if (scene_buffer_reserve(vk,&s->vertices,bytes,VK_BUFFER_USAGE_VERTEX_BUFFER_BIT,err,errlen)) return -1;
		memcpy(s->vertices.mapped,vertices,(size_t)bytes);
	}
	VkDeviceSize uniform_bytes=(VkDeviceSize)draw_count*s->uniform_stride;
	if (uniform_bytes>UINT32_MAX) { seterr(err,errlen,"frame exceeds dynamic uniform offset range",VK_SUCCESS);return -1; }
	if (draw_count) {
		VkDeviceSize previous_capacity=s->uniforms.capacity;
		if (scene_buffer_reserve(vk,&s->uniforms,uniform_bytes,VK_BUFFER_USAGE_UNIFORM_BUFFER_BIT,err,errlen)) return -1;
		if (previous_capacity!=s->uniforms.capacity) {
			VkDescriptorBufferInfo buffer={.buffer=s->uniforms.buffer,.offset=0,.range=sizeof(scene_uniform)};
			VkWriteDescriptorSet write={.sType=VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,.dstSet=s->mesh_descriptor,
				.dstBinding=0,.descriptorCount=1,.descriptorType=VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER_DYNAMIC,.pBufferInfo=&buffer};
			vkUpdateDescriptorSets(vk->device,1,&write,0,NULL);
		}
	}
	for (uint32_t i=0;i<draw_count;i++) {
		const worldr_frame_draw *draw=&draws[i];
		if ((draw->kind==0) && ((uint64_t)draw->first+draw->count>vertex_count||draw->count%3)) {
			seterr(err,errlen,"overlay vertex range invalid",VK_SUCCESS);return -1;
		}
		if (draw->kind==2 || draw->kind==4) {
			if (!scene_find_geometry(vk,draw->geometry)) { seterr(err,errlen,"mesh resource is not resident",VK_SUCCESS);return -1; }
		}
		if (draw->kind==3 || draw->kind==5 || draw->kind==6) {
			if (!*scene_texture_link(vk,draw->geometry)) { seterr(err,errlen,"texture resource is not resident",VK_SUCCESS);return -1; }
		}
		if (draw->kind==2 || draw->kind==3 || draw->kind==4 || draw->kind==5 || draw->kind==6) {
			memcpy((uint8_t *)s->uniforms.mapped+i*s->uniform_stride,draw->projection,sizeof(scene_uniform));
		}
	}
	if(scene_shadow_prepare(vk,(unsigned)shadow_count,err,errlen))return -1;
	if(scene_transparency_prepare(vk,(unsigned)transparency_count,err,errlen))return -1;
	VkDeviceSize pixels=(VkDeviceSize)vk->width*vk->height*4;
	if (out_bgra&&scene_buffer_reserve(vk,&s->readback,pixels,VK_BUFFER_USAGE_TRANSFER_DST_BIT,err,errlen)) return -1;
	uint32_t index=0;
	int suboptimal=0;
	if (vk->mode!=WORLDR_VK_HEADLESS) {
		VkResult r=vkAcquireNextImageKHR(vk->device,vk->swapchain,WORLDR_VK_WAIT_NS,s->acquired,VK_NULL_HANDLE,&index);
		if (r==VK_ERROR_OUT_OF_DATE_KHR) return -2;
		if (r==VK_TIMEOUT||r==VK_NOT_READY) return -3;
		suboptimal=r==VK_SUBOPTIMAL_KHR;
		if (r!=VK_SUCCESS&&r!=VK_SUBOPTIMAL_KHR) { seterr(err,errlen,"scene swapchain acquire failed (2s limit)",r);return -1; }
		if (index>=WORLDR_MAX_IMAGES) { seterr(err,errlen,"scene swapchain index out of range",VK_SUCCESS);return -1; }
	}
	if (scene_begin(vk,err,errlen)) return -1;
	float clear_rgb[3]={clear[0],clear[1],clear[2]};
	float clear_alpha=s->linear_color?fminf(1.0f,fmaxf(0.0f,clear[3])):clear[3];
	if(s->linear_color)for(int i=0;i<3;i++)clear_rgb[i]=(clear_rgb[i]<=0.04045f?clear_rgb[i]/12.92f:powf((clear_rgb[i]+0.055f)/1.055f,2.4f))*clear_alpha;
	float scene_clear[4]={clear_rgb[0],clear_rgb[1],clear_rgb[2],clear_alpha};
	if (shadow_count) scene_shadow_render(vk,draws,shadow_scopes,(unsigned)shadow_count);
	if (glow_count) scene_glow_render(vk,draws,glow_scopes,(unsigned)glow_count);
	if(transparency_count)scene_transparency_render(vk,draws,transparency_scopes,(unsigned)transparency_count,shadow_scopes,(unsigned)shadow_count,scene_clear);
	VkClearValue values[2]={ {.color={.float32={clear_rgb[0],clear_rgb[1],clear_rgb[2],clear_alpha}}},{.depthStencil={1,0}} };
	VkRenderPassBeginInfo pass={.sType=VK_STRUCTURE_TYPE_RENDER_PASS_BEGIN_INFO,
		.renderPass=s->pass,.framebuffer=s->frames[index],.renderArea={.extent={vk->width,vk->height}},
		.clearValueCount=2,.pClearValues=values};
	vkCmdBeginRenderPass(s->cmd,&pass,VK_SUBPASS_CONTENTS_INLINE);
	const float full[4]={0,0,(float)vk->width,(float)vk->height};
	const float extent[2]={(float)vk->width,(float)vk->height};
	VkDescriptorSet active_mesh=s->mesh_descriptor;
	for (uint32_t i=0;i<draw_count;i++) {
		const worldr_frame_draw *draw=&draws[i];
		if(draw->flags&2){
			for(int slot=0;slot<transparency_count;slot++)if(transparency_scopes[slot].first==i)scene_transparency_composite(vk,draw->viewport,(unsigned)slot);
			continue;
		}
		if (draw->kind==0) {
			if (!draw->count) continue;
			scene_set_viewport(vk,full);
			vkCmdBindPipeline(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,s->overlay_pipeline);
			vkCmdBindDescriptorSets(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,s->layout,0,1,&s->descriptor,0,NULL);
			VkDeviceSize offset=0;
			vkCmdBindVertexBuffers(s->cmd,0,1,&s->vertices.buffer,&offset);
			vkCmdPushConstants(s->cmd,s->layout,VK_SHADER_STAGE_VERTEX_BIT,0,sizeof(extent),extent);
			vkCmdDraw(s->cmd,draw->count,1,draw->first,0);
		} else if (draw->kind==1) {
			active_mesh=s->mesh_descriptor;
			for(int slot=0;slot<shadow_count;slot++)if(shadow_scopes[slot].begin==i)active_mesh=s->shadow->descriptors[slot];
			scene_set_viewport(vk,draw->viewport);
			VkClearAttachment clear_depth={.aspectMask=VK_IMAGE_ASPECT_DEPTH_BIT,.clearValue={.depthStencil={1,0}}};
			VkClearRect rect={.rect=scene_scissor(vk,draw->viewport),.baseArrayLayer=0,.layerCount=1};
			if (rect.rect.extent.width&&rect.rect.extent.height) vkCmdClearAttachments(s->cmd,1,&clear_depth,1,&rect);
			for (int slot=0;slot<glow_count;slot++)
				if (glow_scopes[slot].begin==i) scene_glow_composite(vk,draw->viewport,(unsigned)slot);
		} else if (draw->kind==2 || draw->kind==4) {
			scene_geometry *g=scene_find_geometry(vk,draw->geometry);
			scene_set_viewport(vk,draw->viewport);
			vkCmdBindPipeline(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,draw->kind==4?s->mesh_readonly_pipeline:s->mesh_pipeline);
			uint32_t offset=(uint32_t)(i*s->uniform_stride);
			vkCmdBindDescriptorSets(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,s->mesh_layout,0,1,&active_mesh,1,&offset);
			VkDeviceSize vertex_offset=0;
			vkCmdBindVertexBuffers(s->cmd,0,1,&g->storage.buffer,&vertex_offset);
			vkCmdBindIndexBuffer(s->cmd,g->storage.buffer,g->index_offset,VK_INDEX_TYPE_UINT32);
			vkCmdDrawIndexed(s->cmd,g->index_count,1,0,0,0);
		} else if (draw->kind==3 || draw->kind==5 || draw->kind==6) {
			scene_texture *texture=*scene_texture_link(vk,draw->geometry);
			scene_set_viewport(vk,draw->viewport);
			vkCmdBindPipeline(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,draw->kind==6?s->translucent_pipeline:draw->kind==5?s->image_pipeline:s->surface_pipeline);
			uint32_t offset=(uint32_t)(i*s->uniform_stride);
			VkDescriptorSet descriptors[]={active_mesh,texture->descriptor};
			vkCmdBindDescriptorSets(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,s->surface_layout,0,2,descriptors,1,&offset);
			vkCmdDraw(s->cmd,6,1,0,0);
		}
	}
	vkCmdEndRenderPass(s->cmd);
	if (s->output) scene_output_render(vk,index,output_transform);
	if (out_bgra) {
		VkBufferImageCopy copy={.imageSubresource={.aspectMask=VK_IMAGE_ASPECT_COLOR_BIT,.layerCount=1},.imageExtent={vk->width,vk->height,1}};
		vkCmdCopyImageToBuffer(s->cmd,s->output?s->output->readback.image:s->target.image,VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,s->readback.buffer,1,&copy);
		VkBufferMemoryBarrier host={.sType=VK_STRUCTURE_TYPE_BUFFER_MEMORY_BARRIER,
			.srcAccessMask=VK_ACCESS_TRANSFER_WRITE_BIT,.dstAccessMask=VK_ACCESS_HOST_READ_BIT,
			.srcQueueFamilyIndex=VK_QUEUE_FAMILY_IGNORED,.dstQueueFamilyIndex=VK_QUEUE_FAMILY_IGNORED,
			.buffer=s->readback.buffer,.offset=0,.size=pixels};
		vkCmdPipelineBarrier(s->cmd,VK_PIPELINE_STAGE_TRANSFER_BIT,VK_PIPELINE_STAGE_HOST_BIT,0,0,NULL,1,&host,0,NULL);
	}
	VkSemaphore wait=vk->mode!=WORLDR_VK_HEADLESS?s->acquired:VK_NULL_HANDLE;
	VkSemaphore signal=vk->mode!=WORLDR_VK_HEADLESS?s->complete[index]:VK_NULL_HANDLE;
	if (scene_submit(vk,wait,signal,err,errlen)) return -1;
	if (out_bgra) memcpy(out_bgra,s->readback.mapped,(size_t)pixels);
	if (vk->mode!=WORLDR_VK_HEADLESS) {
		VkPresentInfoKHR present={.sType=VK_STRUCTURE_TYPE_PRESENT_INFO_KHR,.waitSemaphoreCount=1,.pWaitSemaphores=&signal,
			.swapchainCount=1,.pSwapchains=&vk->swapchain,.pImageIndices=&index};
		VkResult r=vkQueuePresentKHR(vk->queue,&present);
		if (r==VK_ERROR_OUT_OF_DATE_KHR||r==VK_SUBOPTIMAL_KHR) return -2;
		if (r!=VK_SUCCESS&&r!=VK_SUBOPTIMAL_KHR) { seterr(err,errlen,"scene swapchain present failed",r);return -1; }
	}
	return suboptimal?-2:0;
}
