/* Explicit directional shadow maps. Four camera scopes, fixed 1024-square
 * depth targets, one map per scope, and a nine-tap bounded receiver filter.
 * Uniforms/resources are retained and recreated only on target/mode changes. */
#define SCENE_SHADOW_VIEWS 4
#define SCENE_SHADOW_SIZE 1024

typedef struct { uint32_t begin,end; } scene_shadow_scope;
struct scene_shadow {
 VkRenderPass pass;
 VkPipeline mesh,surface;
 VkDescriptorPool pool;
 VkDescriptorSet descriptors[SCENE_SHADOW_VIEWS];
 VkFramebuffer frames[SCENE_SHADOW_VIEWS];
 scene_image maps[SCENE_SHADOW_VIEWS];
 VkFormat format;
 unsigned slots;
};

static void scene_shadow_release(worldr_vk *vk)
{
 worldr_scene *s=vk->scene;scene_shadow *h=s?s->shadow:NULL;if(!h)return;
 if(h->mesh)vkDestroyPipeline(vk->device,h->mesh,NULL);
 if(h->surface)vkDestroyPipeline(vk->device,h->surface,NULL);
 for(unsigned i=0;i<SCENE_SHADOW_VIEWS;i++){
  if(h->frames[i])vkDestroyFramebuffer(vk->device,h->frames[i],NULL);
  scene_drop_image(vk,&h->maps[i]);
 }
 if(h->pool)vkDestroyDescriptorPool(vk->device,h->pool,NULL);
 if(h->pass)vkDestroyRenderPass(vk->device,h->pass,NULL);
 free(h);s->shadow=NULL;
}

static int scene_shadow_scopes(const worldr_frame_draw *draws,uint32_t count,scene_shadow_scope *scopes,char *err,int errlen)
{
 unsigned active=0;
 for(uint32_t i=0;i<count;i++){
  if(draws[i].kind!=1||draws[i].shadow_params[0]<=0)continue;
  uint32_t end=i+1;while(end<count&&(draws[end].kind==2||draws[end].kind==3||draws[end].kind==4||draws[end].kind==6))end++;
  if(active==SCENE_SHADOW_VIEWS){seterr(err,errlen,"at most 4 shadowed scene commands are supported per frame",VK_SUCCESS);return -1;}
  scopes[active++]=(scene_shadow_scope){i,end};i=end-1;
 }
 return (int)active;
}

static int scene_shadow_pipeline(worldr_vk *vk,int surface,char *err,int errlen)
{
 worldr_scene *s=vk->scene;scene_shadow *h=s->shadow;VkShaderModule vert=VK_NULL_HANDLE;
 VkShaderModuleCreateInfo shader={.sType=VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,
  .codeSize=surface?sizeof(worldr_scene_shadow_surface_vert):sizeof(worldr_scene_shadow_vert),
  .pCode=surface?worldr_scene_shadow_surface_vert:worldr_scene_shadow_vert};
 VkResult r=vkCreateShaderModule(vk->device,&shader,NULL,&vert);if(r!=VK_SUCCESS)goto fail;
 VkPipelineShaderStageCreateInfo stage={.sType=VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,.stage=VK_SHADER_STAGE_VERTEX_BIT,.module=vert,.pName="main"};
 VkVertexInputBindingDescription binding={.binding=0,.stride=sizeof(worldr_mesh_vertex),.inputRate=VK_VERTEX_INPUT_RATE_VERTEX};
 VkVertexInputAttributeDescription attribute={.location=0,.binding=0,.format=VK_FORMAT_R32G32B32_SFLOAT,.offset=offsetof(worldr_mesh_vertex,x)};
 VkPipelineVertexInputStateCreateInfo input={.sType=VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO,
  .vertexBindingDescriptionCount=surface?0:1,.pVertexBindingDescriptions=&binding,.vertexAttributeDescriptionCount=surface?0:1,.pVertexAttributeDescriptions=&attribute};
 VkPipelineInputAssemblyStateCreateInfo assembly={.sType=VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO,.topology=VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST};
 VkViewport viewport={.width=SCENE_SHADOW_SIZE,.height=SCENE_SHADOW_SIZE,.minDepth=0,.maxDepth=1};
 VkRect2D scissor={.extent={SCENE_SHADOW_SIZE,SCENE_SHADOW_SIZE}};
 VkPipelineViewportStateCreateInfo view={.sType=VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO,.viewportCount=1,.pViewports=&viewport,.scissorCount=1,.pScissors=&scissor};
 VkPipelineRasterizationStateCreateInfo raster={.sType=VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO,
  .polygonMode=VK_POLYGON_MODE_FILL,.cullMode=VK_CULL_MODE_NONE,.frontFace=VK_FRONT_FACE_COUNTER_CLOCKWISE,.lineWidth=1};
 VkPipelineMultisampleStateCreateInfo samples={.sType=VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO,.rasterizationSamples=VK_SAMPLE_COUNT_1_BIT};
 VkPipelineDepthStencilStateCreateInfo depth={.sType=VK_STRUCTURE_TYPE_PIPELINE_DEPTH_STENCIL_STATE_CREATE_INFO,.depthTestEnable=VK_TRUE,.depthWriteEnable=VK_TRUE,.depthCompareOp=VK_COMPARE_OP_LESS_OR_EQUAL};
 VkPipelineColorBlendStateCreateInfo blending={.sType=VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO};
 VkGraphicsPipelineCreateInfo pipeline={.sType=VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO,.stageCount=1,.pStages=&stage,
  .pVertexInputState=&input,.pInputAssemblyState=&assembly,.pViewportState=&view,.pRasterizationState=&raster,.pMultisampleState=&samples,
  .pDepthStencilState=&depth,.pColorBlendState=&blending,.layout=s->mesh_layout,.renderPass=h->pass};
 r=vkCreateGraphicsPipelines(vk->device,VK_NULL_HANDLE,1,&pipeline,NULL,surface?&h->surface:&h->mesh);
 if(r!=VK_SUCCESS)goto fail;
 vkDestroyShaderModule(vk->device,vert,NULL);return 0;
 fail:
 if(vert)vkDestroyShaderModule(vk->device,vert,NULL);
 seterr(err,errlen,"shadow pipeline creation failed",r);return -1;
}

static int scene_shadow_prepare(worldr_vk *vk,unsigned count,char *err,int errlen)
{
 if(!count)return 0;
 worldr_scene *s=vk->scene;VkResult r;
 if(!s->shadow){
  s->shadow=calloc(1,sizeof(*s->shadow));if(!s->shadow){seterr(err,errlen,"shadow allocation failed",VK_ERROR_OUT_OF_HOST_MEMORY);return -1;}
  scene_shadow *h=s->shadow;VkFormat formats[]={VK_FORMAT_D32_SFLOAT,VK_FORMAT_D16_UNORM};
  for(unsigned i=0;i<2;i++){
   VkFormatProperties p;vkGetPhysicalDeviceFormatProperties(vk->phys,formats[i],&p);
   VkFormatFeatureFlags required=VK_FORMAT_FEATURE_DEPTH_STENCIL_ATTACHMENT_BIT|VK_FORMAT_FEATURE_SAMPLED_IMAGE_BIT;
   if((p.optimalTilingFeatures&required)==required){h->format=formats[i];break;}
  }
  if(!h->format){r=VK_ERROR_FORMAT_NOT_SUPPORTED;goto fail;}
  VkAttachmentDescription attachment={.format=h->format,.samples=VK_SAMPLE_COUNT_1_BIT,.loadOp=VK_ATTACHMENT_LOAD_OP_CLEAR,.storeOp=VK_ATTACHMENT_STORE_OP_STORE,
   .stencilLoadOp=VK_ATTACHMENT_LOAD_OP_DONT_CARE,.stencilStoreOp=VK_ATTACHMENT_STORE_OP_DONT_CARE,
   .initialLayout=VK_IMAGE_LAYOUT_UNDEFINED,.finalLayout=VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL};
  VkAttachmentReference depth={0,VK_IMAGE_LAYOUT_DEPTH_STENCIL_ATTACHMENT_OPTIMAL};
  VkSubpassDescription subpass={.pipelineBindPoint=VK_PIPELINE_BIND_POINT_GRAPHICS,.pDepthStencilAttachment=&depth};
  VkSubpassDependency dependencies[2]={
   {.srcSubpass=VK_SUBPASS_EXTERNAL,.dstSubpass=0,.srcStageMask=VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT,
    .dstStageMask=VK_PIPELINE_STAGE_EARLY_FRAGMENT_TESTS_BIT|VK_PIPELINE_STAGE_LATE_FRAGMENT_TESTS_BIT,
    .srcAccessMask=VK_ACCESS_SHADER_READ_BIT,.dstAccessMask=VK_ACCESS_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT},
   {.srcSubpass=0,.dstSubpass=VK_SUBPASS_EXTERNAL,.srcStageMask=VK_PIPELINE_STAGE_EARLY_FRAGMENT_TESTS_BIT|VK_PIPELINE_STAGE_LATE_FRAGMENT_TESTS_BIT,
    .dstStageMask=VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT,.srcAccessMask=VK_ACCESS_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT,.dstAccessMask=VK_ACCESS_SHADER_READ_BIT}};
  VkRenderPassCreateInfo pass={.sType=VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO,.attachmentCount=1,.pAttachments=&attachment,
   .subpassCount=1,.pSubpasses=&subpass,.dependencyCount=2,.pDependencies=dependencies};
  if((r=vkCreateRenderPass(vk->device,&pass,NULL,&h->pass))!=VK_SUCCESS)goto fail;
  VkDescriptorPoolSize sizes[2]={{VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER_DYNAMIC,SCENE_SHADOW_VIEWS},{VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,SCENE_SHADOW_VIEWS}};
  VkDescriptorPoolCreateInfo pool={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO,.maxSets=SCENE_SHADOW_VIEWS,.poolSizeCount=2,.pPoolSizes=sizes};
  if((r=vkCreateDescriptorPool(vk->device,&pool,NULL,&h->pool))!=VK_SUCCESS)goto fail;
  if(scene_shadow_pipeline(vk,0,err,errlen)||scene_shadow_pipeline(vk,1,err,errlen))goto resource_fail;
 }
 scene_shadow *h=s->shadow;
 while(h->slots<count){
  unsigned slot=h->slots;
  if(scene_make_image(vk,&h->maps[slot],SCENE_SHADOW_SIZE,SCENE_SHADOW_SIZE,h->format,
   VK_IMAGE_USAGE_DEPTH_STENCIL_ATTACHMENT_BIT|VK_IMAGE_USAGE_SAMPLED_BIT,VK_IMAGE_ASPECT_DEPTH_BIT,err,errlen))goto resource_fail;
  VkFramebufferCreateInfo frame={.sType=VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO,.renderPass=h->pass,
   .attachmentCount=1,.pAttachments=&h->maps[slot].view,.width=SCENE_SHADOW_SIZE,.height=SCENE_SHADOW_SIZE,.layers=1};
  if((r=vkCreateFramebuffer(vk->device,&frame,NULL,&h->frames[slot]))!=VK_SUCCESS)goto fail;
  VkDescriptorSetAllocateInfo allocate={.sType=VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO,.descriptorPool=h->pool,.descriptorSetCount=1,.pSetLayouts=&s->mesh_descriptor_layout};
  if((r=vkAllocateDescriptorSets(vk->device,&allocate,&h->descriptors[slot]))!=VK_SUCCESS)goto fail;
  h->slots++;
 }
 for(unsigned slot=0;slot<count;slot++){
  VkDescriptorBufferInfo buffer={.buffer=s->uniforms.buffer,.offset=0,.range=sizeof(scene_uniform)};
  VkDescriptorImageInfo sampled={.sampler=s->sampler,.imageView=h->maps[slot].view,.imageLayout=VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL};
  VkWriteDescriptorSet writes[2]={
   {.sType=VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,.dstSet=h->descriptors[slot],.dstBinding=0,.descriptorCount=1,.descriptorType=VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER_DYNAMIC,.pBufferInfo=&buffer},
   {.sType=VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,.dstSet=h->descriptors[slot],.dstBinding=1,.descriptorCount=1,.descriptorType=VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,.pImageInfo=&sampled}};
  vkUpdateDescriptorSets(vk->device,2,writes,0,NULL);
 }
 return 0;
 fail:seterr(err,errlen,"shadow resources unavailable",r);
 resource_fail:scene_shadow_release(vk);return -1;
}

static void scene_shadow_render(worldr_vk *vk,const worldr_frame_draw *draws,const scene_shadow_scope *scopes,unsigned count)
{
 worldr_scene *s=vk->scene;scene_shadow *h=s->shadow;
 for(unsigned slot=0;slot<count;slot++){
  VkClearValue clear={.depthStencil={1,0}};
  VkRenderPassBeginInfo pass={.sType=VK_STRUCTURE_TYPE_RENDER_PASS_BEGIN_INFO,.renderPass=h->pass,.framebuffer=h->frames[slot],
   .renderArea={.extent={SCENE_SHADOW_SIZE,SCENE_SHADOW_SIZE}},.clearValueCount=1,.pClearValues=&clear};
  vkCmdBeginRenderPass(s->cmd,&pass,VK_SUBPASS_CONTENTS_INLINE);
  for(uint32_t i=scopes[slot].begin+1;i<scopes[slot].end;i++){
   const worldr_frame_draw *draw=&draws[i];if(!(draw->flags&1))continue;
   uint32_t offset=(uint32_t)(i*s->uniform_stride);
   vkCmdBindDescriptorSets(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,s->mesh_layout,0,1,&s->mesh_descriptor,1,&offset);
   if(draw->kind==3){vkCmdBindPipeline(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,h->surface);vkCmdDraw(s->cmd,6,1,0,0);}
   else if(draw->kind==2||draw->kind==4){
    scene_geometry *g=scene_find_geometry(vk,draw->geometry);VkDeviceSize zero=0;
    vkCmdBindPipeline(s->cmd,VK_PIPELINE_BIND_POINT_GRAPHICS,h->mesh);
    vkCmdBindVertexBuffers(s->cmd,0,1,&g->storage.buffer,&zero);vkCmdBindIndexBuffer(s->cmd,g->storage.buffer,g->index_offset,VK_INDEX_TYPE_UINT32);
    vkCmdDrawIndexed(s->cmd,g->index_count,1,0,0,0);
   }
  }
  vkCmdEndRenderPass(s->cmd);
 }
}
