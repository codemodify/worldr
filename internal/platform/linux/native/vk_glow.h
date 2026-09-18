/* Opt-in authored background halos. Seed/blur work happens before the main
 * pass, whose ordered overlay/scene/image stream and MSAA resolve stay intact.
 * The half-resolution seed reuses the main multisample color and depth views
 * in a smaller framebuffer; both are cleared before full-size main rendering.
 * Only half-resolution single-sample effect images are added. */
#define SCENE_GLOW_VIEWS 4

typedef struct { uint32_t begin, end; } scene_glow_scope;

struct scene_glow {
	VkRenderPass seed_pass, blur_pass;
	VkPipeline mesh, surface, readonly, blur, composite;
	VkPipelineLayout post_layout;
	VkDescriptorPool pool;
	VkDescriptorSet seed_descriptor, horizontal_descriptor, halo_descriptor[SCENE_GLOW_VIEWS];
	scene_image seed, horizontal, halo[SCENE_GLOW_VIEWS];
	VkFramebuffer seed_frame, horizontal_frame, halo_frame[SCENE_GLOW_VIEWS];
	uint32_t width, height, slots;
};

static void scene_glow_release(worldr_vk *vk)
{
	worldr_scene *s=vk->scene;
	scene_glow *g=s?s->glow:NULL;
	if (!g) return;
	if (g->mesh) vkDestroyPipeline(vk->device,g->mesh,NULL);
	if (g->surface) vkDestroyPipeline(vk->device,g->surface,NULL);
	if (g->readonly) vkDestroyPipeline(vk->device,g->readonly,NULL);
	if (g->blur) vkDestroyPipeline(vk->device,g->blur,NULL);
	if (g->composite) vkDestroyPipeline(vk->device,g->composite,NULL);
	if (g->seed_frame) vkDestroyFramebuffer(vk->device,g->seed_frame,NULL);
	if (g->horizontal_frame) vkDestroyFramebuffer(vk->device,g->horizontal_frame,NULL);
	for (unsigned i=0;i<SCENE_GLOW_VIEWS;i++) {
		if (g->halo_frame[i]) vkDestroyFramebuffer(vk->device,g->halo_frame[i],NULL);
		scene_drop_image(vk,&g->halo[i]);
	}
	if (g->pool) vkDestroyDescriptorPool(vk->device,g->pool,NULL);
	if (g->post_layout) vkDestroyPipelineLayout(vk->device,g->post_layout,NULL);
	if (g->seed_pass) vkDestroyRenderPass(vk->device,g->seed_pass,NULL);
	if (g->blur_pass) vkDestroyRenderPass(vk->device,g->blur_pass,NULL);
	scene_drop_image(vk,&g->seed);
	scene_drop_image(vk,&g->horizontal);
	free(g);s->glow=NULL;
}

static int scene_glow_scopes(const worldr_frame_draw *draws,uint32_t count,
	scene_glow_scope scopes[SCENE_GLOW_VIEWS],char *err,int errlen)
{
	unsigned active=0;
	for (uint32_t i=0;i<count;i++) {
		if (draws[i].kind!=1) continue;
		uint32_t end=i+1;
		int emits=0;
		while (end<count && (draws[end].kind==2 || draws[end].kind==3 || draws[end].kind==4 || draws[end].kind==6)) {
			const worldr_frame_draw *draw=&draws[end];
			if ((draw->kind==2 || draw->kind==4) && (draw->glow[0]>0 || draw->glow[1]>0 || draw->glow[2]>0)) emits=1;
			end++;
		}
		if (emits) {
			if (active==SCENE_GLOW_VIEWS) {
				seterr(err,errlen,"at most 4 emitting scene commands are supported per frame",VK_SUCCESS);return -1;
			}
			scopes[active++]=(scene_glow_scope){i,end};
		}
		i=end-1;
	}
	return (int)active;
}

/* kind 0=opaque mesh seed, 1=opaque content occluder, 2=read-only mesh
 * seed, 3=separable blur, 4=background additive composite in the main pass. */
static int scene_glow_pipeline(worldr_vk *vk,int kind,char *err,int errlen)
{
	worldr_scene *s=vk->scene;scene_glow *g=s->glow;
	int surface=kind==1,readonly=kind==2,post=kind>=3,composite=kind==4;
	VkShaderModule vert=VK_NULL_HANDLE,frag=VK_NULL_HANDLE;
	VkShaderModuleCreateInfo shader={.sType=VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,
		.codeSize=post?sizeof(worldr_scene_fullscreen_vert):surface?sizeof(worldr_scene_surface_vert):sizeof(worldr_scene_glow_vert),
		.pCode=post?worldr_scene_fullscreen_vert:surface?worldr_scene_surface_vert:worldr_scene_glow_vert};
	VkResult r=vkCreateShaderModule(vk->device,&shader,NULL,&vert);
	if(r!=VK_SUCCESS)goto done;
	shader.codeSize=composite?sizeof(worldr_scene_glow_composite_frag):post?sizeof(worldr_scene_glow_blur_frag):surface?sizeof(worldr_scene_glow_surface_frag):sizeof(worldr_scene_glow_frag);
	shader.pCode=composite?worldr_scene_glow_composite_frag:post?worldr_scene_glow_blur_frag:surface?worldr_scene_glow_surface_frag:worldr_scene_glow_frag;
	if((r=vkCreateShaderModule(vk->device,&shader,NULL,&frag))!=VK_SUCCESS)goto done;
	VkPipelineShaderStageCreateInfo stages[2]={
		{.sType=VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,.stage=VK_SHADER_STAGE_VERTEX_BIT,.module=vert,.pName="main"},
		{.sType=VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,.stage=VK_SHADER_STAGE_FRAGMENT_BIT,.module=frag,.pName="main"}};
	uint32_t linear_color=(uint32_t)s->linear_color;
	VkSpecializationMapEntry color_entry={.constantID=0,.offset=0,.size=sizeof(linear_color)};
	VkSpecializationInfo color_mode={.mapEntryCount=1,.pMapEntries=&color_entry,.dataSize=sizeof(linear_color),.pData=&linear_color};
	stages[0].pSpecializationInfo=stages[1].pSpecializationInfo=&color_mode;
	VkVertexInputBindingDescription binding={.binding=0,.stride=sizeof(worldr_mesh_vertex),.inputRate=VK_VERTEX_INPUT_RATE_VERTEX};
	VkVertexInputAttributeDescription attributes[2]={
		{.location=0,.binding=0,.format=VK_FORMAT_R32G32B32_SFLOAT,.offset=offsetof(worldr_mesh_vertex,x)},
		{.location=2,.binding=0,.format=VK_FORMAT_R32G32B32A32_SFLOAT,.offset=offsetof(worldr_mesh_vertex,r)}};
	VkPipelineVertexInputStateCreateInfo input={.sType=VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO,
		.vertexBindingDescriptionCount=post||surface?0:1,.pVertexBindingDescriptions=&binding,
		.vertexAttributeDescriptionCount=post||surface?0:2,.pVertexAttributeDescriptions=attributes};
	VkPipelineInputAssemblyStateCreateInfo assembly={.sType=VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO,.topology=VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST};
	VkViewport viewport={.width=(float)vk->width,.height=(float)vk->height,.minDepth=0,.maxDepth=1};
	VkRect2D scissor={.extent={vk->width,vk->height}};
	VkPipelineViewportStateCreateInfo view={.sType=VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO,
		.viewportCount=1,.pViewports=&viewport,.scissorCount=1,.pScissors=&scissor};
	VkPipelineRasterizationStateCreateInfo raster={.sType=VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO,
		.polygonMode=VK_POLYGON_MODE_FILL,.cullMode=VK_CULL_MODE_NONE,.frontFace=VK_FRONT_FACE_COUNTER_CLOCKWISE,.lineWidth=1};
	VkPipelineMultisampleStateCreateInfo samples={.sType=VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO,
		.rasterizationSamples=kind==3?VK_SAMPLE_COUNT_1_BIT:s->samples};
	VkPipelineDepthStencilStateCreateInfo depth={.sType=VK_STRUCTURE_TYPE_PIPELINE_DEPTH_STENCIL_STATE_CREATE_INFO,
		.depthTestEnable=!post,.depthWriteEnable=!post&&!readonly,.depthCompareOp=VK_COMPARE_OP_LESS_OR_EQUAL};
	VkPipelineColorBlendAttachmentState blend={.blendEnable=readonly||composite,
		.srcColorBlendFactor=VK_BLEND_FACTOR_ONE,.dstColorBlendFactor=VK_BLEND_FACTOR_ONE,.colorBlendOp=VK_BLEND_OP_ADD,
		.srcAlphaBlendFactor=VK_BLEND_FACTOR_ZERO,.dstAlphaBlendFactor=VK_BLEND_FACTOR_ONE,.alphaBlendOp=VK_BLEND_OP_ADD,
		.colorWriteMask=VK_COLOR_COMPONENT_R_BIT|VK_COLOR_COMPONENT_G_BIT|VK_COLOR_COMPONENT_B_BIT|VK_COLOR_COMPONENT_A_BIT};
	VkPipelineColorBlendStateCreateInfo blending={.sType=VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO,.attachmentCount=1,.pAttachments=&blend};
	VkDynamicState dynamicStates[]={VK_DYNAMIC_STATE_VIEWPORT,VK_DYNAMIC_STATE_SCISSOR};
	VkPipelineDynamicStateCreateInfo dynamic={.sType=VK_STRUCTURE_TYPE_PIPELINE_DYNAMIC_STATE_CREATE_INFO,.dynamicStateCount=2,.pDynamicStates=dynamicStates};
	VkGraphicsPipelineCreateInfo pipeline={.sType=VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO,.stageCount=2,.pStages=stages,
		.pVertexInputState=&input,.pInputAssemblyState=&assembly,.pViewportState=&view,.pRasterizationState=&raster,
		.pMultisampleState=&samples,.pDepthStencilState=&depth,.pColorBlendState=&blending,.pDynamicState=&dynamic,
		.layout=post?g->post_layout:surface?s->surface_layout:s->mesh_layout,
		.renderPass=composite?s->pass:post?g->blur_pass:g->seed_pass,.subpass=0};
	VkPipeline *out=kind==0?&g->mesh:kind==1?&g->surface:kind==2?&g->readonly:kind==3?&g->blur:&g->composite;
	r=vkCreateGraphicsPipelines(vk->device,VK_NULL_HANDLE,1,&pipeline,NULL,out);
 done:
	if(vert)vkDestroyShaderModule(vk->device,vert,NULL);
	if(frag)vkDestroyShaderModule(vk->device,frag,NULL);
	if(r!=VK_SUCCESS){seterr(err,errlen,"glow graphics pipeline failed",r);return -1;}
	return 0;
}

static int scene_glow_image(worldr_vk *vk,scene_image *image,VkFramebuffer *frame,
	VkDescriptorSet *descriptor,uint32_t width,uint32_t height,VkRenderPass pass,char *err,int errlen)
{
	worldr_scene *s=vk->scene;scene_glow *g=s->glow;
	if(scene_make_image(vk,image,width,height,s->color_format,VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT|VK_IMAGE_USAGE_SAMPLED_BIT,
		VK_IMAGE_ASPECT_COLOR_BIT,err,errlen))return -1;
	VkDescriptorSetAllocateInfo allocate={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO,
		.descriptorPool=g->pool,.descriptorSetCount=1,.pSetLayouts=&s->descriptor_layout};
	VkResult r=vkAllocateDescriptorSets(vk->device,&allocate,descriptor);
	if(r!=VK_SUCCESS){seterr(err,errlen,"glow descriptor allocation failed",r);return -1;}
	VkDescriptorImageInfo sampled={.sampler=s->sampler,.imageView=image->view,.imageLayout=VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL};
	VkWriteDescriptorSet write={.sType=VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,.dstSet=*descriptor,
		.dstBinding=0,.descriptorCount=1,.descriptorType=VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,.pImageInfo=&sampled};
	vkUpdateDescriptorSets(vk->device,1,&write,0,NULL);
	if(frame){
		VkFramebufferCreateInfo create={.sType=VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO,.renderPass=pass,
			.attachmentCount=1,.pAttachments=&image->view,.width=width,.height=height,.layers=1};
		if((r=vkCreateFramebuffer(vk->device,&create,NULL,frame))!=VK_SUCCESS){seterr(err,errlen,"glow framebuffer failed",r);return -1;}
	}
	return 0;
}

static int scene_glow_prepare(worldr_vk *vk,unsigned count,char *err,int errlen)
{
	if(!count)return 0;
	worldr_scene *s=vk->scene;VkResult r;
	if(!s->glow){
		VkFormatProperties props;VkImageFormatProperties image;
		vkGetPhysicalDeviceFormatProperties(vk->phys,s->color_format,&props);
		VkFormatFeatureFlags required=VK_FORMAT_FEATURE_COLOR_ATTACHMENT_BIT|VK_FORMAT_FEATURE_COLOR_ATTACHMENT_BLEND_BIT|
			VK_FORMAT_FEATURE_SAMPLED_IMAGE_BIT|VK_FORMAT_FEATURE_SAMPLED_IMAGE_FILTER_LINEAR_BIT;
		if((props.optimalTilingFeatures&required)!=required ||
			vkGetPhysicalDeviceImageFormatProperties(vk->phys,s->color_format,VK_IMAGE_TYPE_2D,VK_IMAGE_TILING_OPTIMAL,
				VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT|VK_IMAGE_USAGE_SAMPLED_BIT,0,&image)!=VK_SUCCESS ||
			!(image.sampleCounts&VK_SAMPLE_COUNT_1_BIT)||vk->width>image.maxExtent.width||vk->height>image.maxExtent.height){
			seterr(err,errlen,"authored glow requires sampled linear-filterable color targets",VK_ERROR_FORMAT_NOT_SUPPORTED);return -1;
		}
		s->glow=calloc(1,sizeof(*s->glow));
		if(!s->glow){seterr(err,errlen,"glow allocation failed",VK_ERROR_OUT_OF_HOST_MEMORY);return -1;}
		scene_glow *g=s->glow;g->width=(vk->width+1)/2;g->height=(vk->height+1)/2;
		int ms=s->samples!=VK_SAMPLE_COUNT_1_BIT;
		VkAttachmentDescription attachments[3]={
			{.format=s->color_format,.samples=s->samples,.loadOp=VK_ATTACHMENT_LOAD_OP_CLEAR,
			 .storeOp=ms?VK_ATTACHMENT_STORE_OP_DONT_CARE:VK_ATTACHMENT_STORE_OP_STORE,
			 .stencilLoadOp=VK_ATTACHMENT_LOAD_OP_DONT_CARE,.stencilStoreOp=VK_ATTACHMENT_STORE_OP_DONT_CARE,
			 .initialLayout=VK_IMAGE_LAYOUT_UNDEFINED,.finalLayout=ms?VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL:VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL},
			{.format=s->depth_format,.samples=s->samples,.loadOp=VK_ATTACHMENT_LOAD_OP_CLEAR,.storeOp=VK_ATTACHMENT_STORE_OP_DONT_CARE,
			 .stencilLoadOp=VK_ATTACHMENT_LOAD_OP_DONT_CARE,.stencilStoreOp=VK_ATTACHMENT_STORE_OP_DONT_CARE,
			 .initialLayout=VK_IMAGE_LAYOUT_UNDEFINED,.finalLayout=VK_IMAGE_LAYOUT_DEPTH_STENCIL_ATTACHMENT_OPTIMAL},
			{.format=s->color_format,.samples=VK_SAMPLE_COUNT_1_BIT,.loadOp=VK_ATTACHMENT_LOAD_OP_DONT_CARE,.storeOp=VK_ATTACHMENT_STORE_OP_STORE,
			 .stencilLoadOp=VK_ATTACHMENT_LOAD_OP_DONT_CARE,.stencilStoreOp=VK_ATTACHMENT_STORE_OP_DONT_CARE,
			 .initialLayout=VK_IMAGE_LAYOUT_UNDEFINED,.finalLayout=VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL}};
		VkAttachmentReference color={0,VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL},depth={1,VK_IMAGE_LAYOUT_DEPTH_STENCIL_ATTACHMENT_OPTIMAL},resolve={2,VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL};
		VkSubpassDescription subpass={.pipelineBindPoint=VK_PIPELINE_BIND_POINT_GRAPHICS,.colorAttachmentCount=1,.pColorAttachments=&color,
			.pDepthStencilAttachment=&depth,.pResolveAttachments=ms?&resolve:NULL};
		VkSubpassDependency dependencies[2]={
			{.srcSubpass=VK_SUBPASS_EXTERNAL,.dstSubpass=0,
			 .srcStageMask=VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT|VK_PIPELINE_STAGE_EARLY_FRAGMENT_TESTS_BIT|VK_PIPELINE_STAGE_LATE_FRAGMENT_TESTS_BIT|VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT,
			 .dstStageMask=VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT|VK_PIPELINE_STAGE_EARLY_FRAGMENT_TESTS_BIT|VK_PIPELINE_STAGE_LATE_FRAGMENT_TESTS_BIT,
			 .srcAccessMask=VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT|VK_ACCESS_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT|VK_ACCESS_SHADER_READ_BIT,
			 .dstAccessMask=VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT|VK_ACCESS_DEPTH_STENCIL_ATTACHMENT_READ_BIT|VK_ACCESS_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT},
			{.srcSubpass=0,.dstSubpass=VK_SUBPASS_EXTERNAL,
			 .srcStageMask=VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT|VK_PIPELINE_STAGE_EARLY_FRAGMENT_TESTS_BIT|VK_PIPELINE_STAGE_LATE_FRAGMENT_TESTS_BIT,
			 .dstStageMask=VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT|VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT|VK_PIPELINE_STAGE_EARLY_FRAGMENT_TESTS_BIT|VK_PIPELINE_STAGE_LATE_FRAGMENT_TESTS_BIT,
			 .srcAccessMask=VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT|VK_ACCESS_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT,
			 .dstAccessMask=VK_ACCESS_SHADER_READ_BIT|VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT|VK_ACCESS_DEPTH_STENCIL_ATTACHMENT_READ_BIT|VK_ACCESS_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT}};
		VkRenderPassCreateInfo pass={.sType=VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO,.attachmentCount=ms?3:2,.pAttachments=attachments,
			.subpassCount=1,.pSubpasses=&subpass,.dependencyCount=2,.pDependencies=dependencies};
		if((r=vkCreateRenderPass(vk->device,&pass,NULL,&g->seed_pass))!=VK_SUCCESS)goto fail;
		attachments[0].samples=VK_SAMPLE_COUNT_1_BIT;attachments[0].storeOp=VK_ATTACHMENT_STORE_OP_STORE;
		attachments[0].finalLayout=VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL;
		subpass.pDepthStencilAttachment=NULL;subpass.pResolveAttachments=NULL;pass.attachmentCount=1;
		if((r=vkCreateRenderPass(vk->device,&pass,NULL,&g->blur_pass))!=VK_SUCCESS)goto fail;
		VkDescriptorPoolSize size={.type=VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,.descriptorCount=2+SCENE_GLOW_VIEWS};
		VkDescriptorPoolCreateInfo pool={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO,.maxSets=2+SCENE_GLOW_VIEWS,.poolSizeCount=1,.pPoolSizes=&size};
		if((r=vkCreateDescriptorPool(vk->device,&pool,NULL,&g->pool))!=VK_SUCCESS)goto fail;
		VkPushConstantRange push={.stageFlags=VK_SHADER_STAGE_FRAGMENT_BIT,.offset=0,.size=2*sizeof(float)};
		VkPipelineLayoutCreateInfo layout={.sType=VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO,.setLayoutCount=1,.pSetLayouts=&s->descriptor_layout,
			.pushConstantRangeCount=1,.pPushConstantRanges=&push};
		if((r=vkCreatePipelineLayout(vk->device,&layout,NULL,&g->post_layout))!=VK_SUCCESS)goto fail;
		if(scene_glow_image(vk,&g->seed,NULL,&g->seed_descriptor,g->width,g->height,g->seed_pass,err,errlen)||
			scene_glow_image(vk,&g->horizontal,&g->horizontal_frame,&g->horizontal_descriptor,g->width,g->height,g->blur_pass,err,errlen))goto resource_fail;
		VkImageView views[3]={ms?s->multisample.view:g->seed.view,s->depth.view,g->seed.view};
		VkFramebufferCreateInfo frame={.sType=VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO,.renderPass=g->seed_pass,
			.attachmentCount=ms?3:2,.pAttachments=views,.width=g->width,.height=g->height,.layers=1};
		if((r=vkCreateFramebuffer(vk->device,&frame,NULL,&g->seed_frame))!=VK_SUCCESS)goto fail;
		for(int kind=0;kind<5;kind++)if(scene_glow_pipeline(vk,kind,err,errlen))goto resource_fail;
	}
	while(s->glow->slots<count){
		scene_glow *g=s->glow;unsigned slot=g->slots;
		if(scene_glow_image(vk,&g->halo[slot],&g->halo_frame[slot],&g->halo_descriptor[slot],g->width,g->height,g->blur_pass,err,errlen))goto resource_fail;
		g->slots++;
	}
	return 0;
 fail:
	seterr(err,errlen,"glow resource creation failed",r);
 resource_fail:
	scene_glow_release(vk);return -1;
}

static VkRect2D scene_glow_half_scissor(worldr_vk *vk,const float *view,int border)
{
	scene_glow *g=vk->scene->glow;VkRect2D full=scene_scissor(vk,view);
	// Include the texels straddling fractional camera boundaries. The final
	// additive composite is clipped by the exact ordinary scene scissor.
	int x=(int)((uint64_t)full.offset.x*g->width/vk->width)-border;
	int y=(int)((uint64_t)full.offset.y*g->height/vk->height)-border;
	int right=(int)(((uint64_t)(full.offset.x+full.extent.width)*g->width+vk->width-1)/vk->width)+border;
	int bottom=(int)(((uint64_t)(full.offset.y+full.extent.height)*g->height+vk->height-1)/vk->height)+border;
	if(x<0)x=0;
	if(y<0)y=0;
	if(right>(int)g->width)right=(int)g->width;
	if(bottom>(int)g->height)bottom=(int)g->height;
	return(VkRect2D){.offset={x,y},.extent={right>x?(uint32_t)(right-x):0,bottom>y?(uint32_t)(bottom-y):0}};
}

static void scene_glow_blur(worldr_vk *vk,VkFramebuffer frame,VkDescriptorSet source,const float *step,const float *view)
{
	worldr_scene *s=vk->scene;scene_glow *g=s->glow;
	VkClearValue clear={.color={.float32={0,0,0,0}}};
	VkRenderPassBeginInfo pass={.sType=VK_STRUCTURE_TYPE_RENDER_PASS_BEGIN_INFO,.renderPass=g->blur_pass,.framebuffer=frame,
		.renderArea={.extent={g->width,g->height}},.clearValueCount=1,.pClearValues=&clear};
	vkCmdBeginRenderPass(s->cmd,&pass,VK_SUBPASS_CONTENTS_INLINE);
	VkViewport viewport={.width=(float)g->width,.height=(float)g->height,.minDepth=0,.maxDepth=1};
	VkRect2D scissor=scene_glow_half_scissor(vk,view,1);
	vkCmdSetViewport(s->cmd,0,1,&viewport);vkCmdSetScissor(s->cmd,0,1,&scissor);
	vkCmdBindPipeline(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,g->blur);
	vkCmdBindDescriptorSets(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,g->post_layout,0,1,&source,0,NULL);
	vkCmdPushConstants(s->cmd,g->post_layout,VK_SHADER_STAGE_FRAGMENT_BIT,0,2*sizeof(float),step);
	vkCmdDraw(s->cmd,3,1,0,0);
	vkCmdEndRenderPass(s->cmd);
}

static void scene_glow_render(worldr_vk *vk,const worldr_frame_draw *draws,const scene_glow_scope *scopes,unsigned count)
{
	worldr_scene *s=vk->scene;scene_glow *g=s->glow;
	for(unsigned slot=0;slot<count;slot++){
		const scene_glow_scope *scope=&scopes[slot];
		VkClearValue clear[2]={ {.color={.float32={0,0,0,0}}},{.depthStencil={1,0}} };
		VkRenderPassBeginInfo pass={.sType=VK_STRUCTURE_TYPE_RENDER_PASS_BEGIN_INFO,.renderPass=g->seed_pass,.framebuffer=g->seed_frame,
			.renderArea={.extent={g->width,g->height}},.clearValueCount=2,.pClearValues=clear};
		vkCmdBeginRenderPass(s->cmd,&pass,VK_SUBPASS_CONTENTS_INLINE);
		const float *view=draws[scope->begin].viewport;
		float scale_x=(float)g->width/vk->width,scale_y=(float)g->height/vk->height;
		VkViewport viewport={.x=view[0]*scale_x,.y=view[1]*scale_y,.width=view[2]*scale_x,.height=view[3]*scale_y,.minDepth=0,.maxDepth=1};
		VkRect2D scissor=scene_glow_half_scissor(vk,view,0);
		vkCmdSetViewport(s->cmd,0,1,&viewport);vkCmdSetScissor(s->cmd,0,1,&scissor);
		for(uint32_t i=scope->begin+1;i<scope->end;i++){
			const worldr_frame_draw *draw=&draws[i];
			// Translucent content contributes neither an opaque glow occluder nor emission.
			if(draw->kind==6) continue;
			uint32_t offset=(uint32_t)(i*s->uniform_stride);
			if(draw->kind==3){
				vkCmdBindPipeline(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,g->surface);
				vkCmdBindDescriptorSets(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,s->surface_layout,0,1,&s->mesh_descriptor,1,&offset);
				vkCmdDraw(s->cmd,6,1,0,0);
			}else{
				scene_geometry *geometry=scene_find_geometry(vk,draw->geometry);
				vkCmdBindPipeline(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,draw->kind==4?g->readonly:g->mesh);
				vkCmdBindDescriptorSets(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,s->mesh_layout,0,1,&s->mesh_descriptor,1,&offset);
				VkDeviceSize vertex_offset=0;vkCmdBindVertexBuffers(s->cmd,0,1,&geometry->storage.buffer,&vertex_offset);
				vkCmdBindIndexBuffer(s->cmd,geometry->storage.buffer,geometry->index_offset,VK_INDEX_TYPE_UINT32);
				vkCmdDrawIndexed(s->cmd,geometry->index_count,1,0,0,0);
			}
		}
		vkCmdEndRenderPass(s->cmd);
		float horizontal[2]={1.0f/g->width,0},vertical[2]={0,1.0f/g->height};
		scene_glow_blur(vk,g->horizontal_frame,g->seed_descriptor,horizontal,draws[scope->begin].viewport);
		scene_glow_blur(vk,g->halo_frame[slot],g->horizontal_descriptor,vertical,draws[scope->begin].viewport);
	}
}

static void scene_glow_composite(worldr_vk *vk,const float *view,unsigned slot)
{
	worldr_scene *s=vk->scene;scene_glow *g=s->glow;
	VkViewport viewport={.width=(float)vk->width,.height=(float)vk->height,.minDepth=0,.maxDepth=1};
	VkRect2D scissor=scene_scissor(vk,view);
	vkCmdSetViewport(s->cmd,0,1,&viewport);vkCmdSetScissor(s->cmd,0,1,&scissor);
	vkCmdBindPipeline(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,g->composite);
	vkCmdBindDescriptorSets(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,g->post_layout,0,1,&g->halo_descriptor[slot],0,NULL);
	vkCmdDraw(s->cmd,3,1,0,0);
}
