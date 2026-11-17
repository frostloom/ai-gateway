// autoroute body 改写器：RewriteModel 把请求 JSON 对象的 model 字段换成指定模型名，
// 其余键的值语义保持（JSON 解码相等），经 json.RawMessage 取原字节后重序列化。
package autoroute

import (
	"encoding/json"
	"fmt"
)

// RewriteModel 把 JSON 对象 body 中的 "model" 字段替换为 model。
// 其余键的值语义保持不变（JSON 解码后相等）：各值以原始字节经 json.RawMessage 直通，
// 再由 json.Marshal 默认 escapeHTML 重序列化——字符串内的 <、>、& 与 U+2028/2029
// 转义为 \uXXXX，无意义空白被压缩，故这些键的输出字节可能与输入不同。
// body 必须是含 "model" 键的合法 JSON 对象，JSON 无效、顶层非对象或缺键时报错。
// 存在重复 "model" 键时按 map 语义保留最后一处并整体替换为给定值。
func RewriteModel(body []byte, model string) ([]byte, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, fmt.Errorf("autoroute: 请求体不是合法 JSON 对象: %w", err)
	}
	if _, has := top["model"]; !has {
		return nil, fmt.Errorf("autoroute: 请求体缺少 model 字段")
	}
	newModel, err := json.Marshal(model)
	if err != nil {
		return nil, fmt.Errorf("autoroute: 序列化 model 失败: %w", err)
	}
	top["model"] = json.RawMessage(newModel)
	out, err := json.Marshal(top)
	if err != nil {
		return nil, fmt.Errorf("autoroute: 重序列化请求体失败: %w", err)
	}
	return out, nil
}
