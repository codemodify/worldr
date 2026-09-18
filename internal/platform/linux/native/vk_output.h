/* The optional linear renderer writes a floating-point linear target. A separate presentation
 * pass encodes those pixels exactly once on either UNORM or sRGB swapchains,
 * without requiring mutable swapchain formats or transfer-destination support.
 * Headless readback exercises the same output shader into an encoded target. */
struct scene_output {
 scene_image readback;
 VkRenderPass pass;
 VkPipelineLayout layout;
 VkPipeline pipeline;
 VkDescriptorPool pool;
 VkDescriptorSet descriptor;
 VkFramebuffer frames[WORLDR_MAX_IMAGES];
};

static void scene_output_release(worldr_vk *vk)
{
 worldr_scene *s=vk->scene;scene_output *o=s?s->output:NULL;if(!o)return;
 for(unsigned i=0;i<WORLDR_MAX_IMAGES;i++)if(o->frames[i])vkDestroyFramebuffer(vk->device,o->frames[i],NULL);
 if(o->pipeline)vkDestroyPipeline(vk->device,o->pipeline,NULL);
 if(o->layout)vkDestroyPipelineLayout(vk->device,o->layout,NULL);
 if(o->pool)vkDestroyDescriptorPool(vk->device,o->pool,NULL);
 if(o->pass)vkDestroyRenderPass(vk->device,o->pass,NULL);
 scene_drop_image(vk,&o->readback);
 free(o);s->output=NULL;
}

static int scene_output_prepare(worldr_vk *vk,char *err,int errlen)
{
 worldr_scene *s=vk->scene;VkResult r;VkShaderModule vert=VK_NULL_HANDLE,frag=VK_NULL_HANDLE;
 s->output=calloc(1,sizeof(*s->output));if(!s->output){seterr(err,errlen,"linear output allocation failed",VK_ERROR_OUT_OF_HOST_MEMORY);return -1;}
 scene_output *o=s->output;
 VkAttachmentDescription attachment={.format=vk->format,.samples=VK_SAMPLE_COUNT_1_BIT,
  .loadOp=VK_ATTACHMENT_LOAD_OP_DONT_CARE,.storeOp=VK_ATTACHMENT_STORE_OP_STORE,
  .stencilLoadOp=VK_ATTACHMENT_LOAD_OP_DONT_CARE,.stencilStoreOp=VK_ATTACHMENT_STORE_OP_DONT_CARE,
  .initialLayout=VK_IMAGE_LAYOUT_UNDEFINED,.finalLayout=vk->mode==WORLDR_VK_HEADLESS?VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL:VK_IMAGE_LAYOUT_PRESENT_SRC_KHR};
 VkAttachmentReference color={0,VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL};
 VkSubpassDescription subpass={.pipelineBindPoint=VK_PIPELINE_BIND_POINT_GRAPHICS,.colorAttachmentCount=1,.pColorAttachments=&color};
 VkSubpassDependency dependencies[2]={
  {.srcSubpass=VK_SUBPASS_EXTERNAL,.dstSubpass=0,.srcStageMask=VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT,
   .dstStageMask=VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT|VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT,
   .srcAccessMask=VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT,.dstAccessMask=VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT|VK_ACCESS_SHADER_READ_BIT},
  {.srcSubpass=0,.dstSubpass=VK_SUBPASS_EXTERNAL,.srcStageMask=VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT,
   .dstStageMask=vk->mode==WORLDR_VK_HEADLESS?VK_PIPELINE_STAGE_TRANSFER_BIT:VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT,.srcAccessMask=VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT,.dstAccessMask=vk->mode==WORLDR_VK_HEADLESS?VK_ACCESS_TRANSFER_READ_BIT:0}};
 VkRenderPassCreateInfo pass={.sType=VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO,.attachmentCount=1,.pAttachments=&attachment,
  .subpassCount=1,.pSubpasses=&subpass,.dependencyCount=2,.pDependencies=dependencies};
 if((r=vkCreateRenderPass(vk->device,&pass,NULL,&o->pass))!=VK_SUCCESS)goto fail;
 VkPushConstantRange push={.stageFlags=VK_SHADER_STAGE_FRAGMENT_BIT,.offset=0,.size=8*sizeof(float)};
 VkPipelineLayoutCreateInfo layout={.sType=VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO,.setLayoutCount=1,.pSetLayouts=&s->descriptor_layout,
  .pushConstantRangeCount=1,.pPushConstantRanges=&push};
 if((r=vkCreatePipelineLayout(vk->device,&layout,NULL,&o->layout))!=VK_SUCCESS)goto fail;
 VkDescriptorPoolSize size={VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,1};
 VkDescriptorPoolCreateInfo pool={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO,.maxSets=1,.poolSizeCount=1,.pPoolSizes=&size};
 if((r=vkCreateDescriptorPool(vk->device,&pool,NULL,&o->pool))!=VK_SUCCESS)goto fail;
 VkDescriptorSetAllocateInfo allocate={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO,.descriptorPool=o->pool,.descriptorSetCount=1,.pSetLayouts=&s->descriptor_layout};
 if((r=vkAllocateDescriptorSets(vk->device,&allocate,&o->descriptor))!=VK_SUCCESS)goto fail;
 VkDescriptorImageInfo sampled={.sampler=s->sampler,.imageView=s->target.view,.imageLayout=VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL};
 VkWriteDescriptorSet write={.sType=VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,.dstSet=o->descriptor,.dstBinding=0,
  .descriptorCount=1,.descriptorType=VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,.pImageInfo=&sampled};
 vkUpdateDescriptorSets(vk->device,1,&write,0,NULL);
 if(vk->mode==WORLDR_VK_HEADLESS&&scene_make_image(vk,&o->readback,vk->width,vk->height,vk->format,VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT|VK_IMAGE_USAGE_TRANSFER_SRC_BIT,VK_IMAGE_ASPECT_COLOR_BIT,err,errlen))goto resource_fail;
 unsigned count=vk->mode==WORLDR_VK_HEADLESS?1:vk->image_count;
 for(unsigned i=0;i<count;i++){
  VkFramebufferCreateInfo frame={.sType=VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO,.renderPass=o->pass,
   .attachmentCount=1,.pAttachments=vk->mode==WORLDR_VK_HEADLESS?&o->readback.view:&s->views[i],.width=vk->width,.height=vk->height,.layers=1};
  if((r=vkCreateFramebuffer(vk->device,&frame,NULL,&o->frames[i]))!=VK_SUCCESS)goto fail;
 }
 VkShaderModuleCreateInfo shader={.sType=VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,.codeSize=sizeof(worldr_scene_fullscreen_vert),.pCode=worldr_scene_fullscreen_vert};
 if((r=vkCreateShaderModule(vk->device,&shader,NULL,&vert))!=VK_SUCCESS)goto fail;
 shader.codeSize=sizeof(worldr_scene_present_frag);shader.pCode=worldr_scene_present_frag;
 if((r=vkCreateShaderModule(vk->device,&shader,NULL,&frag))!=VK_SUCCESS)goto fail;
 uint32_t srgb=vk->format==VK_FORMAT_B8G8R8A8_SRGB||vk->format==VK_FORMAT_R8G8B8A8_SRGB;
 VkSpecializationMapEntry entry={.constantID=1,.offset=0,.size=sizeof(srgb)};
 VkSpecializationInfo mode={.mapEntryCount=1,.pMapEntries=&entry,.dataSize=sizeof(srgb),.pData=&srgb};
 VkPipelineShaderStageCreateInfo stages[2]={
  {.sType=VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,.stage=VK_SHADER_STAGE_VERTEX_BIT,.module=vert,.pName="main"},
  {.sType=VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,.stage=VK_SHADER_STAGE_FRAGMENT_BIT,.module=frag,.pName="main",.pSpecializationInfo=&mode}};
 VkPipelineVertexInputStateCreateInfo input={.sType=VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO};
 VkPipelineInputAssemblyStateCreateInfo assembly={.sType=VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO,.topology=VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST};
 VkViewport viewport={.width=(float)vk->width,.height=(float)vk->height,.minDepth=0,.maxDepth=1};
 VkRect2D scissor={.extent={vk->width,vk->height}};
 VkPipelineViewportStateCreateInfo view={.sType=VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO,.viewportCount=1,.pViewports=&viewport,.scissorCount=1,.pScissors=&scissor};
 VkPipelineRasterizationStateCreateInfo raster={.sType=VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO,.polygonMode=VK_POLYGON_MODE_FILL,.cullMode=VK_CULL_MODE_NONE,.frontFace=VK_FRONT_FACE_COUNTER_CLOCKWISE,.lineWidth=1};
 VkPipelineMultisampleStateCreateInfo samples={.sType=VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO,.rasterizationSamples=VK_SAMPLE_COUNT_1_BIT};
 VkPipelineColorBlendAttachmentState blend={.colorWriteMask=VK_COLOR_COMPONENT_R_BIT|VK_COLOR_COMPONENT_G_BIT|VK_COLOR_COMPONENT_B_BIT|VK_COLOR_COMPONENT_A_BIT};
 VkPipelineColorBlendStateCreateInfo blending={.sType=VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO,.attachmentCount=1,.pAttachments=&blend};
 VkGraphicsPipelineCreateInfo pipeline={.sType=VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO,.stageCount=2,.pStages=stages,
  .pVertexInputState=&input,.pInputAssemblyState=&assembly,.pViewportState=&view,.pRasterizationState=&raster,
  .pMultisampleState=&samples,.pColorBlendState=&blending,.layout=o->layout,.renderPass=o->pass};
 r=vkCreateGraphicsPipelines(vk->device,VK_NULL_HANDLE,1,&pipeline,NULL,&o->pipeline);
 if(r!=VK_SUCCESS)goto fail;
 vkDestroyShaderModule(vk->device,vert,NULL);vkDestroyShaderModule(vk->device,frag,NULL);return 0;
 fail:
 seterr(err,errlen,"linear output creation failed",r);
 resource_fail:
 if(vert)vkDestroyShaderModule(vk->device,vert,NULL);
 if(frag)vkDestroyShaderModule(vk->device,frag,NULL);
 scene_output_release(vk);return -1;
}

static void scene_output_render(worldr_vk *vk,uint32_t index,const float *transform)
{
 worldr_scene *s=vk->scene;scene_output *o=s->output;
 VkRenderPassBeginInfo pass={.sType=VK_STRUCTURE_TYPE_RENDER_PASS_BEGIN_INFO,.renderPass=o->pass,.framebuffer=o->frames[index],.renderArea={.extent={vk->width,vk->height}}};
 vkCmdBeginRenderPass(s->cmd,&pass,VK_SUBPASS_CONTENTS_INLINE);
 vkCmdBindPipeline(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,o->pipeline);
 vkCmdBindDescriptorSets(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,o->layout,0,1,&o->descriptor,0,NULL);
 vkCmdPushConstants(s->cmd,o->layout,VK_SHADER_STAGE_FRAGMENT_BIT,0,8*sizeof(float),transform);
 vkCmdDraw(s->cmd,3,1,0,0);vkCmdEndRenderPass(s->cmd);
}
