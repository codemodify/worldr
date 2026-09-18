/* Front-to-back depth peeling: exact pixel order through a bounded number of
 * transparent layers, including intersecting planes and self-overlapping meshes.
 * Scratch attachments are shared across camera scopes; only accumulated color
 * is retained per scope. Cost is bounded by four views and 32 geometry passes. */
#define SCENE_TRANSPARENCY_VIEWS 4

typedef struct { uint32_t begin,end,first,layers; } scene_transparency_scope;
struct scene_transparency {
 VkRenderPass opaque_pass,peel_pass,accumulate_pass,clear_pass;
 VkPipeline opaque_mesh,opaque_surface,peel_mesh,peel_surface,accumulate,composite;
 VkPipelineLayout peel_layout,post_layout;
 VkDescriptorSetLayout depth_layout;
 VkDescriptorPool pool;
 VkSampler nearest;
 VkDescriptorSet depths[2],layer_descriptor,accumulated[SCENE_TRANSPARENCY_VIEWS];
 scene_image opaque,backdrop,depth[2],layer,result[SCENE_TRANSPARENCY_VIEWS];
 VkFramebuffer opaque_frame,peel_frames[2],result_frames[SCENE_TRANSPARENCY_VIEWS];
 VkFormat depth_format;
 unsigned slots;
};

static void scene_transparency_release(worldr_vk *vk)
{
 worldr_scene *s=vk->scene;scene_transparency *p=s?s->transparency:NULL;if(!p)return;
 VkPipeline pipelines[]={p->opaque_mesh,p->opaque_surface,p->peel_mesh,p->peel_surface,p->accumulate,p->composite};
 for(unsigned i=0;i<6;i++)if(pipelines[i])vkDestroyPipeline(vk->device,pipelines[i],NULL);
 if(p->opaque_frame)vkDestroyFramebuffer(vk->device,p->opaque_frame,NULL);
 for(unsigned i=0;i<2;i++){if(p->peel_frames[i])vkDestroyFramebuffer(vk->device,p->peel_frames[i],NULL);scene_drop_image(vk,&p->depth[i]);}
 for(unsigned i=0;i<SCENE_TRANSPARENCY_VIEWS;i++){if(p->result_frames[i])vkDestroyFramebuffer(vk->device,p->result_frames[i],NULL);scene_drop_image(vk,&p->result[i]);}
 scene_drop_image(vk,&p->opaque);scene_drop_image(vk,&p->backdrop);scene_drop_image(vk,&p->layer);
 if(p->pool)vkDestroyDescriptorPool(vk->device,p->pool,NULL);
 if(p->peel_layout)vkDestroyPipelineLayout(vk->device,p->peel_layout,NULL);
 if(p->post_layout)vkDestroyPipelineLayout(vk->device,p->post_layout,NULL);
 if(p->depth_layout)vkDestroyDescriptorSetLayout(vk->device,p->depth_layout,NULL);
 if(p->nearest)vkDestroySampler(vk->device,p->nearest,NULL);
 VkRenderPass passes[]={p->opaque_pass,p->peel_pass,p->accumulate_pass,p->clear_pass};
 for(unsigned i=0;i<4;i++)if(passes[i])vkDestroyRenderPass(vk->device,passes[i],NULL);
 free(p);s->transparency=NULL;
}

static int scene_transparency_scopes(const worldr_frame_draw *draws,uint32_t count,scene_transparency_scope *scopes,char *err,int errlen)
{
 unsigned active=0;
 for(uint32_t i=0;i<count;i++){
  if(draws[i].kind!=1)continue;
  uint32_t end=i+1,first=0;
  while(end<count&&(draws[end].kind==2||draws[end].kind==3||draws[end].kind==4||draws[end].kind==6)){
   if(!first&&(draws[end].flags&2))first=end;
   end++;
  }
  if(first){
   if(active==SCENE_TRANSPARENCY_VIEWS){seterr(err,errlen,"at most 4 translucent scene commands are supported per frame",VK_SUCCESS);return -1;}
   scopes[active++]=(scene_transparency_scope){i,end,first,draws[i].count};
  }
  i=end-1;
 }
 return (int)active;
}

/* kind0/1 opaque depth mesh/quad; 2/3 peel mesh/quad; 4 accumulate; 5 composite. */
static int scene_transparency_pipeline(worldr_vk *vk,int kind,char *err,int errlen)
{
 worldr_scene *s=vk->scene;scene_transparency *p=s->transparency;
 int post=kind>=4,surface=kind==1||kind==3,peel=kind==2||kind==3;
 VkShaderModule vert=VK_NULL_HANDLE,frag=VK_NULL_HANDLE;VkResult r;
 VkShaderModuleCreateInfo shader={.sType=VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,
  .codeSize=post?sizeof(worldr_scene_fullscreen_vert):surface?sizeof(worldr_scene_surface_vert):sizeof(worldr_scene_mesh_vert),
  .pCode=post?worldr_scene_fullscreen_vert:surface?worldr_scene_surface_vert:worldr_scene_mesh_vert};
 if((r=vkCreateShaderModule(vk->device,&shader,NULL,&vert))!=VK_SUCCESS)goto fail;
 shader.codeSize=post?sizeof(worldr_scene_composite_frag):peel?(surface?sizeof(worldr_scene_peel_surface_frag):sizeof(worldr_scene_peel_mesh_frag)):(surface?sizeof(worldr_scene_surface_frag):sizeof(worldr_scene_mesh_frag));
 shader.pCode=post?worldr_scene_composite_frag:peel?(surface?worldr_scene_peel_surface_frag:worldr_scene_peel_mesh_frag):(surface?worldr_scene_surface_frag:worldr_scene_mesh_frag);
 if((r=vkCreateShaderModule(vk->device,&shader,NULL,&frag))!=VK_SUCCESS)goto fail;
 uint32_t linear=(uint32_t)s->linear_color;
 VkSpecializationMapEntry entry={.constantID=0,.offset=0,.size=sizeof(linear)};
 VkSpecializationInfo mode={.mapEntryCount=1,.pMapEntries=&entry,.dataSize=sizeof(linear),.pData=&linear};
 VkPipelineShaderStageCreateInfo stages[2]={
  {.sType=VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,.stage=VK_SHADER_STAGE_VERTEX_BIT,.module=vert,.pName="main",.pSpecializationInfo=&mode},
  {.sType=VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,.stage=VK_SHADER_STAGE_FRAGMENT_BIT,.module=frag,.pName="main",.pSpecializationInfo=&mode}};
 VkVertexInputBindingDescription binding={.binding=0,.stride=sizeof(worldr_mesh_vertex),.inputRate=VK_VERTEX_INPUT_RATE_VERTEX};
 VkVertexInputAttributeDescription attributes[4]={
  {0,0,VK_FORMAT_R32G32B32_SFLOAT,offsetof(worldr_mesh_vertex,x)},
  {1,0,VK_FORMAT_R32G32B32_SFLOAT,offsetof(worldr_mesh_vertex,nx)},
  {2,0,VK_FORMAT_R32G32B32A32_SFLOAT,offsetof(worldr_mesh_vertex,r)},
  {3,0,VK_FORMAT_R32G32B32_SFLOAT,offsetof(worldr_mesh_vertex,bx)}};
 VkPipelineVertexInputStateCreateInfo input={.sType=VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO,
  .vertexBindingDescriptionCount=post||surface?0:1,.pVertexBindingDescriptions=&binding,.vertexAttributeDescriptionCount=post||surface?0:4,.pVertexAttributeDescriptions=attributes};
 VkPipelineInputAssemblyStateCreateInfo assembly={.sType=VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO,.topology=VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST};
 VkViewport viewport={.width=(float)vk->width,.height=(float)vk->height,.minDepth=0,.maxDepth=1};VkRect2D scissor={.extent={vk->width,vk->height}};
 VkPipelineViewportStateCreateInfo view={.sType=VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO,.viewportCount=1,.pViewports=&viewport,.scissorCount=1,.pScissors=&scissor};
 VkPipelineRasterizationStateCreateInfo raster={.sType=VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO,.polygonMode=VK_POLYGON_MODE_FILL,.cullMode=VK_CULL_MODE_NONE,.frontFace=VK_FRONT_FACE_COUNTER_CLOCKWISE,.lineWidth=1};
 VkPipelineMultisampleStateCreateInfo samples={.sType=VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO,.rasterizationSamples=kind==5?s->samples:VK_SAMPLE_COUNT_1_BIT};
 VkPipelineDepthStencilStateCreateInfo depth={.sType=VK_STRUCTURE_TYPE_PIPELINE_DEPTH_STENCIL_STATE_CREATE_INFO,.depthTestEnable=!post,.depthWriteEnable=!post,.depthCompareOp=VK_COMPARE_OP_LESS};
 VkPipelineColorBlendAttachmentState blend={.blendEnable=post,
  .srcColorBlendFactor=kind==4?VK_BLEND_FACTOR_ONE_MINUS_DST_ALPHA:VK_BLEND_FACTOR_ONE,
  .dstColorBlendFactor=kind==4?VK_BLEND_FACTOR_ONE:VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA,.colorBlendOp=VK_BLEND_OP_ADD,
  .srcAlphaBlendFactor=kind==4?VK_BLEND_FACTOR_ONE_MINUS_DST_ALPHA:VK_BLEND_FACTOR_ONE,
  .dstAlphaBlendFactor=kind==4?VK_BLEND_FACTOR_ONE:VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA,.alphaBlendOp=VK_BLEND_OP_ADD,
  .colorWriteMask=VK_COLOR_COMPONENT_R_BIT|VK_COLOR_COMPONENT_G_BIT|VK_COLOR_COMPONENT_B_BIT|VK_COLOR_COMPONENT_A_BIT};
 VkPipelineColorBlendStateCreateInfo blending={.sType=VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO,.attachmentCount=1,.pAttachments=&blend};
 VkDynamicState states[]={VK_DYNAMIC_STATE_VIEWPORT,VK_DYNAMIC_STATE_SCISSOR};
 VkPipelineDynamicStateCreateInfo dynamic={.sType=VK_STRUCTURE_TYPE_PIPELINE_DYNAMIC_STATE_CREATE_INFO,.dynamicStateCount=2,.pDynamicStates=states};
 VkGraphicsPipelineCreateInfo pipeline={.sType=VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO,.stageCount=2,.pStages=stages,
  .pVertexInputState=&input,.pInputAssemblyState=&assembly,.pViewportState=&view,.pRasterizationState=&raster,.pMultisampleState=&samples,
  .pDepthStencilState=&depth,.pColorBlendState=&blending,.pDynamicState=&dynamic,
  .layout=post?p->post_layout:peel?p->peel_layout:surface?s->surface_layout:s->mesh_layout,
  .renderPass=kind==5?s->pass:post?p->accumulate_pass:peel?p->peel_pass:p->opaque_pass};
 VkPipeline *output=kind==0?&p->opaque_mesh:kind==1?&p->opaque_surface:kind==2?&p->peel_mesh:kind==3?&p->peel_surface:kind==4?&p->accumulate:&p->composite;
 r=vkCreateGraphicsPipelines(vk->device,VK_NULL_HANDLE,1,&pipeline,NULL,output);if(r!=VK_SUCCESS)goto fail;
 vkDestroyShaderModule(vk->device,vert,NULL);vkDestroyShaderModule(vk->device,frag,NULL);return 0;
 fail:if(vert)vkDestroyShaderModule(vk->device,vert,NULL);if(frag)vkDestroyShaderModule(vk->device,frag,NULL);
 seterr(err,errlen,"transparency pipeline failed",r);return -1;
}

static int scene_transparency_pass(worldr_vk *vk,int kind,VkRenderPass *out,char *err,int errlen)
{
 worldr_scene *s=vk->scene;scene_transparency *p=s->transparency;int opaque=kind==0,peel=kind==1,spatial=opaque||peel;
 VkAttachmentDescription attachments[2]={
  {.format=s->color_format,.samples=VK_SAMPLE_COUNT_1_BIT,
   .loadOp=kind==2?VK_ATTACHMENT_LOAD_OP_LOAD:VK_ATTACHMENT_LOAD_OP_CLEAR,.storeOp=VK_ATTACHMENT_STORE_OP_STORE,
   .stencilLoadOp=VK_ATTACHMENT_LOAD_OP_DONT_CARE,.stencilStoreOp=VK_ATTACHMENT_STORE_OP_DONT_CARE,
   .initialLayout=kind==2?VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL:VK_IMAGE_LAYOUT_UNDEFINED,.finalLayout=VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL},
  {.format=p->depth_format,.samples=VK_SAMPLE_COUNT_1_BIT,.loadOp=VK_ATTACHMENT_LOAD_OP_CLEAR,.storeOp=VK_ATTACHMENT_STORE_OP_STORE,
   .stencilLoadOp=VK_ATTACHMENT_LOAD_OP_DONT_CARE,.stencilStoreOp=VK_ATTACHMENT_STORE_OP_DONT_CARE,
   .initialLayout=VK_IMAGE_LAYOUT_UNDEFINED,.finalLayout=VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL}};
 VkAttachmentReference color={0,VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL},depth={1,VK_IMAGE_LAYOUT_DEPTH_STENCIL_ATTACHMENT_OPTIMAL};
 VkSubpassDescription subpass={.pipelineBindPoint=VK_PIPELINE_BIND_POINT_GRAPHICS,.colorAttachmentCount=1,.pColorAttachments=&color,.pDepthStencilAttachment=spatial?&depth:NULL};
 VkPipelineStageFlags stages=VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT|VK_PIPELINE_STAGE_EARLY_FRAGMENT_TESTS_BIT|VK_PIPELINE_STAGE_LATE_FRAGMENT_TESTS_BIT;
 VkAccessFlags write=VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT|VK_ACCESS_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT;
 VkSubpassDependency dependencies[2]={
  {.srcSubpass=VK_SUBPASS_EXTERNAL,.dstSubpass=0,.srcStageMask=stages|VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT,.dstStageMask=stages,
   .srcAccessMask=write|VK_ACCESS_SHADER_READ_BIT,.dstAccessMask=write|VK_ACCESS_COLOR_ATTACHMENT_READ_BIT|VK_ACCESS_DEPTH_STENCIL_ATTACHMENT_READ_BIT},
  {.srcSubpass=0,.dstSubpass=VK_SUBPASS_EXTERNAL,.srcStageMask=stages,.dstStageMask=stages|VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT,
   .srcAccessMask=write,.dstAccessMask=VK_ACCESS_SHADER_READ_BIT|write}};
 VkRenderPassCreateInfo pass={.sType=VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO,.attachmentCount=spatial?2:1,.pAttachments=attachments,
  .subpassCount=1,.pSubpasses=&subpass,.dependencyCount=2,.pDependencies=dependencies};
 VkResult r=vkCreateRenderPass(vk->device,&pass,NULL,out);if(r!=VK_SUCCESS){seterr(err,errlen,"transparency render pass failed",r);return -1;}return 0;
}

static int scene_transparency_color(worldr_vk *vk,scene_image *image,VkDescriptorSet *descriptor,char *err,int errlen)
{
 worldr_scene *s=vk->scene;scene_transparency *p=s->transparency;
 if(scene_make_image(vk,image,vk->width,vk->height,s->color_format,VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT|VK_IMAGE_USAGE_SAMPLED_BIT,VK_IMAGE_ASPECT_COLOR_BIT,err,errlen))return -1;
 VkDescriptorSetAllocateInfo allocate={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO,.descriptorPool=p->pool,.descriptorSetCount=1,.pSetLayouts=&s->descriptor_layout};
 VkResult r=vkAllocateDescriptorSets(vk->device,&allocate,descriptor);if(r!=VK_SUCCESS){seterr(err,errlen,"transparency descriptor failed",r);return -1;}
 VkDescriptorImageInfo sampled={.sampler=s->sampler,.imageView=image->view,.imageLayout=VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL};
 VkWriteDescriptorSet write={.sType=VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,.dstSet=*descriptor,.dstBinding=0,.descriptorCount=1,.descriptorType=VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,.pImageInfo=&sampled};
 vkUpdateDescriptorSets(vk->device,1,&write,0,NULL);return 0;
}

static int scene_transparency_prepare(worldr_vk *vk,unsigned count,char *err,int errlen)
{
 if(!count)return 0;
 worldr_scene *s=vk->scene;VkResult r;
 // Three depth images, one opaque backdrop, a current layer, and one result
 // per active camera. Depth uses at most four bytes; linear color uses eight,
 // legacy uses four. The shared backdrop is reused after each scope has baked
 // its transparent result.
 if((uint64_t)vk->width*vk->height*(12+(s->linear_color?8:4)*(2+count))>256ull*1024*1024){seterr(err,errlen,"transparency targets exceed 256 MiB budget",VK_ERROR_OUT_OF_DEVICE_MEMORY);return -1;}
 if(!s->transparency){
  s->transparency=calloc(1,sizeof(*s->transparency));if(!s->transparency){seterr(err,errlen,"transparency allocation failed",VK_ERROR_OUT_OF_HOST_MEMORY);return -1;}
  scene_transparency *p=s->transparency;VkFormat formats[]={VK_FORMAT_D32_SFLOAT,VK_FORMAT_D16_UNORM};
  for(unsigned i=0;i<2;i++){VkFormatProperties props;vkGetPhysicalDeviceFormatProperties(vk->phys,formats[i],&props);
   VkFormatFeatureFlags required=VK_FORMAT_FEATURE_DEPTH_STENCIL_ATTACHMENT_BIT|VK_FORMAT_FEATURE_SAMPLED_IMAGE_BIT;
   if((props.optimalTilingFeatures&required)==required){p->depth_format=formats[i];break;}}
  if(!p->depth_format){r=VK_ERROR_FORMAT_NOT_SUPPORTED;goto fail;}
  if(scene_transparency_pass(vk,0,&p->opaque_pass,err,errlen)||scene_transparency_pass(vk,1,&p->peel_pass,err,errlen)||
   scene_transparency_pass(vk,2,&p->accumulate_pass,err,errlen)||scene_transparency_pass(vk,3,&p->clear_pass,err,errlen))goto resource_fail;
  VkDescriptorSetLayoutBinding bindings[3]={
   {.binding=0,.descriptorType=VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,.descriptorCount=1,.stageFlags=VK_SHADER_STAGE_FRAGMENT_BIT},
   {.binding=1,.descriptorType=VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,.descriptorCount=1,.stageFlags=VK_SHADER_STAGE_FRAGMENT_BIT},
   {.binding=2,.descriptorType=VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,.descriptorCount=1,.stageFlags=VK_SHADER_STAGE_FRAGMENT_BIT}};
  VkDescriptorSetLayoutCreateInfo layout={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO,.bindingCount=3,.pBindings=bindings};
  if((r=vkCreateDescriptorSetLayout(vk->device,&layout,NULL,&p->depth_layout))!=VK_SUCCESS)goto fail;
  VkDescriptorSetLayout sets[]={s->mesh_descriptor_layout,s->descriptor_layout,p->depth_layout};
  VkPushConstantRange push={.stageFlags=VK_SHADER_STAGE_FRAGMENT_BIT,.size=sizeof(int32_t)};
  VkPipelineLayoutCreateInfo pipeline_layout={.sType=VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO,.setLayoutCount=3,.pSetLayouts=sets,.pushConstantRangeCount=1,.pPushConstantRanges=&push};
  if((r=vkCreatePipelineLayout(vk->device,&pipeline_layout,NULL,&p->peel_layout))!=VK_SUCCESS)goto fail;
  pipeline_layout.setLayoutCount=1;pipeline_layout.pSetLayouts=&s->descriptor_layout;pipeline_layout.pushConstantRangeCount=0;
  if((r=vkCreatePipelineLayout(vk->device,&pipeline_layout,NULL,&p->post_layout))!=VK_SUCCESS)goto fail;
  VkDescriptorPoolSize pool_size={VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,7+SCENE_TRANSPARENCY_VIEWS};
  VkDescriptorPoolCreateInfo pool={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO,.maxSets=3+SCENE_TRANSPARENCY_VIEWS,.poolSizeCount=1,.pPoolSizes=&pool_size};
  if((r=vkCreateDescriptorPool(vk->device,&pool,NULL,&p->pool))!=VK_SUCCESS)goto fail;
  VkSamplerCreateInfo sampler={.sType=VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO,.magFilter=VK_FILTER_NEAREST,.minFilter=VK_FILTER_NEAREST,
   .mipmapMode=VK_SAMPLER_MIPMAP_MODE_NEAREST,.addressModeU=VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE,.addressModeV=VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE,.addressModeW=VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE};
  if((r=vkCreateSampler(vk->device,&sampler,NULL,&p->nearest))!=VK_SUCCESS)goto fail;
  if(scene_make_image(vk,&p->opaque,vk->width,vk->height,p->depth_format,VK_IMAGE_USAGE_DEPTH_STENCIL_ATTACHMENT_BIT|VK_IMAGE_USAGE_SAMPLED_BIT,VK_IMAGE_ASPECT_DEPTH_BIT,err,errlen))goto resource_fail;
  if(scene_make_image(vk,&p->backdrop,vk->width,vk->height,s->color_format,VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT|VK_IMAGE_USAGE_SAMPLED_BIT,VK_IMAGE_ASPECT_COLOR_BIT,err,errlen))goto resource_fail;
  VkImageView opaque_views[]={p->backdrop.view,p->opaque.view};
  VkFramebufferCreateInfo frame={.sType=VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO,.renderPass=p->opaque_pass,.attachmentCount=2,.pAttachments=opaque_views,.width=vk->width,.height=vk->height,.layers=1};
  if((r=vkCreateFramebuffer(vk->device,&frame,NULL,&p->opaque_frame))!=VK_SUCCESS)goto fail;
  if(scene_transparency_color(vk,&p->layer,&p->layer_descriptor,err,errlen))goto resource_fail;
  for(unsigned i=0;i<2;i++){
   if(scene_make_image(vk,&p->depth[i],vk->width,vk->height,p->depth_format,VK_IMAGE_USAGE_DEPTH_STENCIL_ATTACHMENT_BIT|VK_IMAGE_USAGE_SAMPLED_BIT,VK_IMAGE_ASPECT_DEPTH_BIT,err,errlen))goto resource_fail;
   VkImageView views[]={p->layer.view,p->depth[i].view};frame.renderPass=p->peel_pass;frame.attachmentCount=2;frame.pAttachments=views;
   if((r=vkCreateFramebuffer(vk->device,&frame,NULL,&p->peel_frames[i]))!=VK_SUCCESS)goto fail;
   VkDescriptorSetAllocateInfo allocate={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO,.descriptorPool=p->pool,.descriptorSetCount=1,.pSetLayouts=&p->depth_layout};
   if((r=vkAllocateDescriptorSets(vk->device,&allocate,&p->depths[i]))!=VK_SUCCESS)goto fail;
  }
  for(unsigned i=0;i<2;i++){
   VkDescriptorImageInfo images[3]={{.sampler=p->nearest,.imageView=p->opaque.view,.imageLayout=VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL},
    {.sampler=p->nearest,.imageView=p->depth[1-i].view,.imageLayout=VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL},
    {.sampler=s->sampler,.imageView=p->backdrop.view,.imageLayout=VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL}};
   VkWriteDescriptorSet writes[3];memset(writes,0,sizeof(writes));for(unsigned binding=0;binding<3;binding++)writes[binding]=(VkWriteDescriptorSet){.sType=VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,.dstSet=p->depths[i],.dstBinding=binding,.descriptorCount=1,.descriptorType=VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,.pImageInfo=&images[binding]};
   vkUpdateDescriptorSets(vk->device,3,writes,0,NULL);
  }
  for(int kind=0;kind<6;kind++)if(scene_transparency_pipeline(vk,kind,err,errlen))goto resource_fail;
 }
 scene_transparency *p=s->transparency;
 while(p->slots<count){unsigned slot=p->slots;
  if(scene_transparency_color(vk,&p->result[slot],&p->accumulated[slot],err,errlen))goto resource_fail;
  VkFramebufferCreateInfo frame={.sType=VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO,.renderPass=p->accumulate_pass,
   .attachmentCount=1,.pAttachments=&p->result[slot].view,.width=vk->width,.height=vk->height,.layers=1};
  if((r=vkCreateFramebuffer(vk->device,&frame,NULL,&p->result_frames[slot]))!=VK_SUCCESS)goto fail;
  p->slots++;
 }
 return 0;
 fail:seterr(err,errlen,"transparency resource creation failed",r);
 resource_fail:scene_transparency_release(vk);return -1;
}

static void scene_transparency_draw(worldr_vk *vk,const worldr_frame_draw *draw,uint32_t index,int peel,VkDescriptorSet mesh)
{
 worldr_scene *s=vk->scene;scene_transparency *p=s->transparency;int surface=draw->kind==3||draw->kind==6;
 VkPipelineLayout layout=peel?p->peel_layout:surface?s->surface_layout:s->mesh_layout;
 vkCmdBindPipeline(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,peel?(surface?p->peel_surface:p->peel_mesh):(surface?p->opaque_surface:p->opaque_mesh));
 uint32_t offset=(uint32_t)(index*s->uniform_stride);
 vkCmdBindDescriptorSets(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,layout,0,1,&mesh,1,&offset);
 if(surface){
  scene_texture *texture=*scene_texture_link(vk,draw->geometry);vkCmdBindDescriptorSets(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,layout,1,1,&texture->descriptor,0,NULL);
  vkCmdDraw(s->cmd,6,1,0,0);
 }else{
  scene_geometry *g=scene_find_geometry(vk,draw->geometry);VkDeviceSize zero=0;
  vkCmdBindVertexBuffers(s->cmd,0,1,&g->storage.buffer,&zero);vkCmdBindIndexBuffer(s->cmd,g->storage.buffer,g->index_offset,VK_INDEX_TYPE_UINT32);
  vkCmdDrawIndexed(s->cmd,g->index_count,1,0,0,0);
 }
}

static void scene_transparency_full_view(worldr_vk *vk,const float *view)
{
 VkViewport viewport={.width=(float)vk->width,.height=(float)vk->height,.minDepth=0,.maxDepth=1};
 VkRect2D scissor=scene_scissor(vk,view);vkCmdSetViewport(vk->scene->cmd,0,1,&viewport);vkCmdSetScissor(vk->scene->cmd,0,1,&scissor);
}

static void scene_transparency_render(worldr_vk *vk,const worldr_frame_draw *draws,const scene_transparency_scope *scopes,unsigned count,const scene_shadow_scope *shadows,unsigned shadow_count,const float *clear)
{
 worldr_scene *s=vk->scene;scene_transparency *p=s->transparency;
 for(unsigned slot=0;slot<count;slot++){
  const scene_transparency_scope *scope=&scopes[slot];const float *view=draws[scope->begin].viewport;
  VkDescriptorSet mesh=s->mesh_descriptor;for(unsigned k=0;k<shadow_count;k++)if(shadows[k].begin==scope->begin)mesh=s->shadow->descriptors[k];
  VkClearValue opaque_clears[2]={ {.color={.float32={clear[0],clear[1],clear[2],clear[3]}}},{.depthStencil={1,0}} };
  VkRenderPassBeginInfo pass={.sType=VK_STRUCTURE_TYPE_RENDER_PASS_BEGIN_INFO,.renderPass=p->opaque_pass,.framebuffer=p->opaque_frame,
   .renderArea={.extent={vk->width,vk->height}},.clearValueCount=2,.pClearValues=opaque_clears};
  vkCmdBeginRenderPass(s->cmd,&pass,VK_SUBPASS_CONTENTS_INLINE);scene_set_viewport(vk,view);
  for(uint32_t i=scope->begin+1;i<scope->end;i++)if(draws[i].kind==2||draws[i].kind==3)scene_transparency_draw(vk,&draws[i],i,0,mesh);
  vkCmdEndRenderPass(s->cmd);
  // Initialize the previous depth's layout even on the first layer; Vulkan
  // descriptor validity does not depend on which dynamic branch samples it.
  VkImageMemoryBarrier initialize={.sType=VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER,.oldLayout=VK_IMAGE_LAYOUT_UNDEFINED,.newLayout=VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
   .srcQueueFamilyIndex=VK_QUEUE_FAMILY_IGNORED,.dstQueueFamilyIndex=VK_QUEUE_FAMILY_IGNORED,.image=p->depth[1].image,
   .subresourceRange={.aspectMask=VK_IMAGE_ASPECT_DEPTH_BIT,.levelCount=1,.layerCount=1},.srcAccessMask=VK_ACCESS_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT|VK_ACCESS_SHADER_READ_BIT,.dstAccessMask=VK_ACCESS_SHADER_READ_BIT};
  vkCmdPipelineBarrier(s->cmd,VK_PIPELINE_STAGE_EARLY_FRAGMENT_TESTS_BIT|VK_PIPELINE_STAGE_LATE_FRAGMENT_TESTS_BIT|VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT,VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT,0,0,NULL,0,NULL,1,&initialize);
  for(uint32_t layer=0;layer<scope->layers;layer++){
   unsigned current=layer%2;VkClearValue clears[2]={ {.color={.float32={0,0,0,0}}},{.depthStencil={1,0}} };
   pass.renderPass=p->peel_pass;pass.framebuffer=p->peel_frames[current];pass.clearValueCount=2;pass.pClearValues=clears;
   vkCmdBeginRenderPass(s->cmd,&pass,VK_SUBPASS_CONTENTS_INLINE);scene_set_viewport(vk,view);
   int32_t first=layer==0;vkCmdPushConstants(s->cmd,p->peel_layout,VK_SHADER_STAGE_FRAGMENT_BIT,0,sizeof(first),&first);
   vkCmdBindDescriptorSets(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,p->peel_layout,2,1,&p->depths[current],0,NULL);
   for(uint32_t i=scope->first;i<scope->end;i++)if(draws[i].flags&2)scene_transparency_draw(vk,&draws[i],i,1,mesh);
   vkCmdEndRenderPass(s->cmd);
   pass.renderPass=layer==0?p->clear_pass:p->accumulate_pass;pass.framebuffer=p->result_frames[slot];pass.clearValueCount=1;pass.pClearValues=clears;
   vkCmdBeginRenderPass(s->cmd,&pass,VK_SUBPASS_CONTENTS_INLINE);scene_transparency_full_view(vk,view);
   vkCmdBindPipeline(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,p->accumulate);
   vkCmdBindDescriptorSets(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,p->post_layout,0,1,&p->layer_descriptor,0,NULL);
   vkCmdDraw(s->cmd,3,1,0,0);vkCmdEndRenderPass(s->cmd);
  }
 }
}

static void scene_transparency_composite(worldr_vk *vk,const float *view,unsigned slot)
{
 worldr_scene *s=vk->scene;scene_transparency *p=s->transparency;scene_transparency_full_view(vk,view);
 vkCmdBindPipeline(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,p->composite);
 vkCmdBindDescriptorSets(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,p->post_layout,0,1,&p->accumulated[slot],0,NULL);
 vkCmdDraw(s->cmd,3,1,0,0);
}
