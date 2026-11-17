// autoroute body 改写器：RewriteModel 把请求 JSON 对象的 model 字段换成指定模型名，
// 其余键的值经 json.RawMessage 逐字节保留后重序列化（输出键序按字母排序）。
package autoroute

import (
	"encoding/json"
	"fmt"
)

// RewriteModel 把 JSON 对象 body 中的 "model" 字段替换为 model，
// 其余键的值逐字节保留：各值以原始字节经 json.RawMessage 直通，不重新编码。
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
