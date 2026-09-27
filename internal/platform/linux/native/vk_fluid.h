/* Ordered procedural fluid surfaces. Only bounded uniform data changes during
 * interaction; no image upload, geometry rebuild, or intermediate target. */
_Static_assert(sizeof(worldr_fluid_field)==880,"fluid std140 uniform size changed");
_Static_assert(offsetof(worldr_fluid_field,surfaces)==112,"fluid surface offset changed");

struct scene_fluid {
	VkPipeline pipeline;
	VkPipelineLayout layout;
	VkDescriptorSetLayout descriptor_layout;
	VkDescriptorPool pool;
	VkDescriptorSet descriptor;
	scene_buffer uniforms;
	VkDeviceSize stride;
};

static void scene_fluid_release(worldr_vk *vk)
{
	worldr_scene *s=vk->scene;
	if(!s || !s->fluid)return;
	scene_fluid *f=s->fluid;
	if(f->pipeline)vkDestroyPipeline(vk->device,f->pipeline,NULL);
	if(f->layout)vkDestroyPipelineLayout(vk->device,f->layout,NULL);
	if(f->pool)vkDestroyDescriptorPool(vk->device,f->pool,NULL);
	if(f->descriptor_layout)vkDestroyDescriptorSetLayout(vk->device,f->descriptor_layout,NULL);
	scene_drop_buffer(vk,&f->uniforms);
	free(f);s->fluid=NULL;
}

static int scene_fluid_pipeline(worldr_vk *vk,char *err,int errlen)
{
	worldr_scene *s=vk->scene;scene_fluid *f=s->fluid;
	VkShaderModule vert=VK_NULL_HANDLE,frag=VK_NULL_HANDLE;
	VkShaderModuleCreateInfo shader={.sType=VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,
		.codeSize=sizeof(worldr_scene_fullscreen_vert),.pCode=worldr_scene_fullscreen_vert};
	VkResult r=vkCreateShaderModule(vk->device,&shader,NULL,&vert);
	if(r!=VK_SUCCESS)goto done;
	shader.codeSize=sizeof(worldr_scene_fluid_frag);shader.pCode=worldr_scene_fluid_frag;
	if((r=vkCreateShaderModule(vk->device,&shader,NULL,&frag))!=VK_SUCCESS)goto done;
	uint32_t linear=(uint32_t)s->linear_color;
	VkSpecializationMapEntry entry={.constantID=0,.offset=0,.size=sizeof(linear)};
	VkSpecializationInfo mode={.mapEntryCount=1,.pMapEntries=&entry,.dataSize=sizeof(linear),.pData=&linear};
	VkPipelineShaderStageCreateInfo stages[2]={
		{.sType=VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,.stage=VK_SHADER_STAGE_VERTEX_BIT,.module=vert,.pName="main"},
		{.sType=VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,.stage=VK_SHADER_STAGE_FRAGMENT_BIT,.module=frag,.pName="main",.pSpecializationInfo=&mode}};
	VkPipelineVertexInputStateCreateInfo input={.sType=VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO};
	VkPipelineInputAssemblyStateCreateInfo assembly={.sType=VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO,.topology=VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST};
	VkPipelineViewportStateCreateInfo view={.sType=VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO,.viewportCount=1,.scissorCount=1};
	VkPipelineRasterizationStateCreateInfo raster={.sType=VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO,
		.polygonMode=VK_POLYGON_MODE_FILL,.cullMode=VK_CULL_MODE_NONE,.frontFace=VK_FRONT_FACE_COUNTER_CLOCKWISE,.lineWidth=1};
	VkPipelineMultisampleStateCreateInfo samples={.sType=VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO,.rasterizationSamples=s->samples};
	VkPipelineDepthStencilStateCreateInfo depth={.sType=VK_STRUCTURE_TYPE_PIPELINE_DEPTH_STENCIL_STATE_CREATE_INFO};
	VkPipelineColorBlendAttachmentState blend={.colorWriteMask=VK_COLOR_COMPONENT_R_BIT|VK_COLOR_COMPONENT_G_BIT|VK_COLOR_COMPONENT_B_BIT|VK_COLOR_COMPONENT_A_BIT};
	VkPipelineColorBlendStateCreateInfo blending={.sType=VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO,.attachmentCount=1,.pAttachments=&blend};
	VkDynamicState states[]={VK_DYNAMIC_STATE_VIEWPORT,VK_DYNAMIC_STATE_SCISSOR};
	VkPipelineDynamicStateCreateInfo dynamic={.sType=VK_STRUCTURE_TYPE_PIPELINE_DYNAMIC_STATE_CREATE_INFO,.dynamicStateCount=2,.pDynamicStates=states};
	VkGraphicsPipelineCreateInfo pipeline={.sType=VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO,.stageCount=2,.pStages=stages,
		.pVertexInputState=&input,.pInputAssemblyState=&assembly,.pViewportState=&view,.pRasterizationState=&raster,
		.pMultisampleState=&samples,.pDepthStencilState=&depth,.pColorBlendState=&blending,.pDynamicState=&dynamic,
		.layout=f->layout,.renderPass=s->pass};
	r=vkCreateGraphicsPipelines(vk->device,VK_NULL_HANDLE,1,&pipeline,NULL,&f->pipeline);
 done:
	if(vert)vkDestroyShaderModule(vk->device,vert,NULL);
	if(frag)vkDestroyShaderModule(vk->device,frag,NULL);
	if(r!=VK_SUCCESS){seterr(err,errlen,"fluid pipeline creation failed",r);return -1;}
	return 0;
}

static int scene_fluid_prepare(worldr_vk *vk,const worldr_fluid_field *fields,uint32_t count,char *err,int errlen)
{
	if(!count)return 0;
	worldr_scene *s=vk->scene;VkResult r;
	if(!s->fluid){
		s->fluid=calloc(1,sizeof(*s->fluid));
		if(!s->fluid){seterr(err,errlen,"fluid allocation failed",VK_ERROR_OUT_OF_HOST_MEMORY);return -1;}
		scene_fluid *f=s->fluid;
		VkPhysicalDeviceProperties properties;vkGetPhysicalDeviceProperties(vk->phys,&properties);
		VkDeviceSize alignment=properties.limits.minUniformBufferOffsetAlignment;
		f->stride=(sizeof(worldr_fluid_field)+alignment-1)/alignment*alignment;
		VkDescriptorSetLayoutBinding binding={.binding=0,.descriptorType=VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER_DYNAMIC,.descriptorCount=1,.stageFlags=VK_SHADER_STAGE_FRAGMENT_BIT};
		VkDescriptorSetLayoutCreateInfo descriptor_layout={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO,.bindingCount=1,.pBindings=&binding};
		if((r=vkCreateDescriptorSetLayout(vk->device,&descriptor_layout,NULL,&f->descriptor_layout))!=VK_SUCCESS)goto fail;
		VkDescriptorPoolSize size={.type=VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER_DYNAMIC,.descriptorCount=1};
		VkDescriptorPoolCreateInfo pool={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO,.maxSets=1,.poolSizeCount=1,.pPoolSizes=&size};
		if((r=vkCreateDescriptorPool(vk->device,&pool,NULL,&f->pool))!=VK_SUCCESS)goto fail;
		VkDescriptorSetAllocateInfo allocate={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO,.descriptorPool=f->pool,.descriptorSetCount=1,.pSetLayouts=&f->descriptor_layout};
		if((r=vkAllocateDescriptorSets(vk->device,&allocate,&f->descriptor))!=VK_SUCCESS)goto fail;
		VkPipelineLayoutCreateInfo layout={.sType=VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO,.setLayoutCount=1,.pSetLayouts=&f->descriptor_layout};
		if((r=vkCreatePipelineLayout(vk->device,&layout,NULL,&f->layout))!=VK_SUCCESS)goto fail;
		if(scene_fluid_pipeline(vk,err,errlen)){scene_fluid_release(vk);return -1;}
	}
	scene_fluid *f=s->fluid;
	VkDeviceSize bytes=(VkDeviceSize)count*f->stride;
	if(bytes>UINT32_MAX){seterr(err,errlen,"fluid fields exceed dynamic uniform range",VK_SUCCESS);return -1;}
	VkDeviceSize capacity=f->uniforms.capacity;
	if(scene_buffer_reserve(vk,&f->uniforms,bytes,VK_BUFFER_USAGE_UNIFORM_BUFFER_BIT,err,errlen))return -1;
	if(capacity!=f->uniforms.capacity){
		VkDescriptorBufferInfo buffer={.buffer=f->uniforms.buffer,.range=sizeof(worldr_fluid_field)};
		VkWriteDescriptorSet write={.sType=VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,.dstSet=f->descriptor,.dstBinding=0,
			.descriptorCount=1,.descriptorType=VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER_DYNAMIC,.pBufferInfo=&buffer};
		vkUpdateDescriptorSets(vk->device,1,&write,0,NULL);
	}
	for(uint32_t i=0;i<count;i++)memcpy((uint8_t *)f->uniforms.mapped+i*f->stride,&fields[i],sizeof(*fields));
	return 0;
 fail:
	seterr(err,errlen,"fluid descriptor allocation failed",r);scene_fluid_release(vk);return -1;
}

static void scene_fluid_render(worldr_vk *vk,const worldr_fluid_field *field,uint32_t index)
{
	worldr_scene *s=vk->scene;scene_fluid *f=s->fluid;
	VkRect2D scissor=scene_scissor(vk,field->bounds);
	if(!scissor.extent.width || !scissor.extent.height)return;
	VkViewport viewport={.width=(float)vk->width,.height=(float)vk->height,.minDepth=0,.maxDepth=1};
	vkCmdSetViewport(s->cmd,0,1,&viewport);vkCmdSetScissor(s->cmd,0,1,&scissor);
	vkCmdBindPipeline(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,f->pipeline);
	uint32_t offset=(uint32_t)(index*f->stride);
	vkCmdBindDescriptorSets(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,f->layout,0,1,&f->descriptor,1,&offset);
	vkCmdDraw(s->cmd,3,1,0,0);
}
