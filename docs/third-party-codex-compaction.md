# 第三方模型的 Codex 网关摘要压缩

当上游支持普通 Responses 请求、但不能解析 Codex 的 `compaction_trigger` 时，可以为账号启用网关摘要压缩。上游还需要提供可调用该模型的 Chat Completions 接口。

在账号已有的 `extra` 中设置以下字段，并保留其他已有配置：

```json
{
  "openai_chat_compaction_enabled": true
}
```

该能力按账号生效，不维护模型名白名单。账号下新增模型，只需要正常配置模型映射、分组准入清单和额度；不需要为每个新模型增加压缩代码。

启用后的请求行为：

- 普通请求继续使用账号原来的协议和模型路由。
- HTTP `/v1/responses` 中包含 `compaction_trigger` 时，使用网关摘要压缩。
- 旧版 `/v1/responses/compact` 使用同一摘要实现。
- WebSocket 的 HTTP bridge 在每一轮检测压缩请求，因此也覆盖连接中途发起的压缩。
- 第一轮就是压缩请求时，会选择 HTTP bridge；本功能不承诺在原生上游 WebSocket 连接中途切换协议。仅提供 HTTP Responses 的第三方上游应使用 HTTP bridge。

摘要请求会去掉压缩控制项、工具定义和内联工具目录，要求上游只返回摘要。网关将完成的摘要封装为认证加密的 `compaction` 项。后续请求只展开网关自己的摘要格式，再交回账号原来的转发路径；原生 OpenAI 加密历史保持原样。

摘要未完整完成或返回空内容时会报错，客户端应保留原始上下文。加密密钥派生自实例的固定 JWT secret；修改该 secret 后旧网关摘要将无法解密。

本功能只对显式启用的 API Key 兼容账号生效。Kimi 等模型已有的普通请求兼容规则独立保留。模型本身仍须在上游的 Chat Completions 入口可用，启用开关不能补出上游缺失的模型或协议。
