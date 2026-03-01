// Day 09 - Protobuf 进阶实践
// 本示例深入演示 Protocol Buffers 的高级特性
// 包括 oneof、嵌套消息、Map、字段兼容性、枚举、repeated、性能优化
//
// ============================================================
// 如果使用真正的 Protobuf，.proto 文件定义如下：
// ============================================================
//
//   syntax = "proto3";
//   package aiplatform;
//
//   enum ModelStatus {
//     MODEL_STATUS_UNKNOWN = 0;    // proto3 要求 0 值
//     MODEL_STATUS_LOADING = 1;
//     MODEL_STATUS_READY = 2;
//     MODEL_STATUS_ERROR = 3;
//     MODEL_STATUS_DEPRECATED = 4;
//   }
//
//   message InferenceRequest {
//     string request_id = 1;
//     string model_name = 2;
//     oneof input {                // 只能设置其中一个
//       TextInput text = 3;
//       ImageInput image = 4;
//       AudioInput audio = 5;
//     }
//     map<string, string> metadata = 6;
//     repeated string tags = 7;
//   }
//
//   message TextInput  { string content = 1; string language = 2; }
//   message ImageInput { bytes data = 1; string format = 2; int32 width = 3; int32 height = 4; }
//   message AudioInput { bytes data = 1; int32 sample_rate = 2; int32 channels = 3; }
//
//   // ModelInfo - V1 只有 name/version，V2 新增其他字段
//   message ModelInfo {
//     string name = 1;
//     string version = 2;
//     ModelStatus status = 3;           // V2 新增
//     int64 parameters = 4;             // V2 新增
//     repeated string capabilities = 5; // V2 新增
//     // reserved 6;                    // 字段 6 已删除
//     map<string, string> labels = 7;   // V2 新增
//   }
//
// ============================================================

package main
import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)
// ============================================================
// 枚举类型 - 使用 Go iota 模拟 Protobuf 枚举
// ============================================================
// ModelStatus 模拟 Protobuf 枚举（proto3 要求第一个值为 0）
type ModelStatus int32
const (
	ModelStatusUnknown    ModelStatus = iota // 0 - 未知（默认值）
	ModelStatusLoading                       // 1 - 加载中
	ModelStatusReady                         // 2 - 就绪
	ModelStatusError                         // 3 - 错误
	ModelStatusDeprecated                    // 4 - 已弃用
)
// String 返回枚举的字符串表示（Protobuf 生成的代码也包含此方法）
func (s ModelStatus) String() string {
	names := [...]string{"UNKNOWN", "LOADING", "READY", "ERROR", "DEPRECATED"}
	if int(s) < len(names) {
		return "MODEL_STATUS_" + names[s]
	}
	return fmt.Sprintf("MODEL_STATUS_%d", int32(s))
}
// ============================================================
// oneof 字段 - 使用 Go 接口模拟联合类型（同一时刻只设一个字段）
// ============================================================
// InputType 是 oneof 的接口标记（sealed interface 模式）
type InputType interface {
	isInputType() // 私有方法，sealed interface 模式
	TypeName() string
}
// TextInput 文本输入（嵌套消息）
type TextInput struct {
	Content  string `json:"content"`
	Language string `json:"language"`
}
func (*TextInput) isInputType()     {}
func (*TextInput) TypeName() string { return "TextInput" }
// ImageInput 图片输入（嵌套消息）
type ImageInput struct {
	Data   []byte `json:"data"`
	Format string `json:"format"`
	Width  int32  `json:"width"`
	Height int32  `json:"height"`
}
func (*ImageInput) isInputType()     {}
func (*ImageInput) TypeName() string { return "ImageInput" }
// AudioInput 音频输入（嵌套消息）
type AudioInput struct {
	Data       []byte `json:"data"`
	SampleRate int32  `json:"sample_rate"`
	Channels   int32  `json:"channels"`
}
func (*AudioInput) isInputType()     {}
func (*AudioInput) TypeName() string { return "AudioInput" }
// --- 嵌套消息、Map 与 Repeated 字段 ---
// InferenceRequest 推理请求 - 综合 oneof、map、repeated
type InferenceRequest struct {
	RequestID string            `json:"request_id"` // 字段 1
	ModelName string            `json:"model_name"` // 字段 2
	Input     InputType         `json:"-"`          // oneof（字段 3/4/5）
	Metadata  map[string]string `json:"metadata"`   // map（字段 6）
	Tags      []string          `json:"tags"`       // repeated（字段 7）
}
// ModelInfo 模型信息 - 用于演示字段兼容性
type ModelInfo struct {
	Name         string            `json:"name"`         // 字段 1（V1）
	Version      string            `json:"version"`      // 字段 2（V1）
	Status       ModelStatus       `json:"status"`       // 字段 3（V2 新增）
	Parameters   int64             `json:"parameters"`   // 字段 4（V2 新增）
	Capabilities []string          `json:"capabilities"` // 字段 5（V2 新增）
	Labels       map[string]string `json:"labels"`       // 字段 7（跳过 6）
}
// ============================================================
// 简化 TLV 编解码器（模拟 Protobuf wire format）
// 格式：[字段编号(1B)][线缆类型(1B)][数据...]
// ============================================================
const (
	wireVarint  byte = 0 // 变长整数编码
	wireFixed64 byte = 1 // 固定 8 字节
	wireBytes   byte = 2 // 带长度前缀的字节序列
	wireFixed32 byte = 5 // 固定 4 字节
)
// TLVEncoder 简化的 TLV 编码器
type TLVEncoder struct{ buf *bytes.Buffer }
func NewTLVEncoder() *TLVEncoder {
	return &TLVEncoder{buf: new(bytes.Buffer)}
}
func NewTLVEncoderWithBuffer(buf *bytes.Buffer) *TLVEncoder {
	buf.Reset()
	return &TLVEncoder{buf: buf}
}
func (e *TLVEncoder) writeTag(fieldNum, wireType byte) {
	e.buf.WriteByte(fieldNum)
	e.buf.WriteByte(wireType)
}
// WriteString 编码字符串：tag + uint32 长度 + 内容
func (e *TLVEncoder) WriteString(fieldNum byte, s string) {
	if s == "" {
		return // proto3 默认值不编码
	}
	e.writeTag(fieldNum, wireBytes)
	_ = binary.Write(e.buf, binary.LittleEndian, uint32(len(s)))
	e.buf.WriteString(s)
}
// WriteInt32 使用 Varint 编码 int32
func (e *TLVEncoder) WriteInt32(fieldNum byte, v int32) {
	if v == 0 {
		return
	}
	e.writeTag(fieldNum, wireVarint)
	e.writeVarint(uint64(v))
}
// WriteInt64 使用 Fixed64 编码 int64
func (e *TLVEncoder) WriteInt64(fieldNum byte, v int64) {
	if v == 0 {
		return
	}
	e.writeTag(fieldNum, wireFixed64)
	_ = binary.Write(e.buf, binary.LittleEndian, v)
}
// writeVarint 变长整数编码 - 每字节低 7 位存数据，最高位为续传标志
func (e *TLVEncoder) writeVarint(v uint64) {
	for v >= 0x80 {
		e.buf.WriteByte(byte(v&0x7F) | 0x80)
		v >>= 7
	}
	e.buf.WriteByte(byte(v))
}
func (e *TLVEncoder) Bytes() []byte { return e.buf.Bytes() }
// TLVDecoder 简化的 TLV 解码器
type TLVDecoder struct {
	data []byte
	pos  int
}
func NewTLVDecoder(data []byte) *TLVDecoder { return &TLVDecoder{data: data} }
func (d *TLVDecoder) HasMore() bool         { return d.pos < len(d.data) }
func (d *TLVDecoder) ReadTag() (fieldNum, wireType byte, err error) {
	if d.pos+2 > len(d.data) {
		return 0, 0, fmt.Errorf("数据不足: 剩余 %d 字节", len(d.data)-d.pos)
	}
	fieldNum, wireType = d.data[d.pos], d.data[d.pos+1]
	d.pos += 2
	return
}
func (d *TLVDecoder) ReadString() (string, error) {
	if d.pos+4 > len(d.data) {
		return "", fmt.Errorf("数据不足: 无法读取长度")
	}
	length := binary.LittleEndian.Uint32(d.data[d.pos : d.pos+4])
	d.pos += 4
	if d.pos+int(length) > len(d.data) {
		return "", fmt.Errorf("数据不足: 需要 %d 字节", length)
	}
	s := string(d.data[d.pos : d.pos+int(length)])
	d.pos += int(length)
	return s, nil
}
func (d *TLVDecoder) ReadVarint() (uint64, error) {
	var result uint64
	var shift uint
	for {
		if d.pos >= len(d.data) {
			return 0, fmt.Errorf("Varint 未终止")
		}
		b := d.data[d.pos]
		d.pos++
		result |= uint64(b&0x7F) << shift
		if b < 0x80 {
			return result, nil
		}
		shift += 7
	}
}
// SkipField 跳过未知字段 - 向前兼容的关键机制
func (d *TLVDecoder) SkipField(wireType byte) error {
	switch wireType {
	case wireVarint:
		_, err := d.ReadVarint()
		return err
	case wireFixed64:
		d.pos += 8
	case wireBytes:
		if d.pos+4 > len(d.data) {
			return fmt.Errorf("无法读取长度前缀")
		}
		length := binary.LittleEndian.Uint32(d.data[d.pos : d.pos+4])
		d.pos += 4 + int(length)
	case wireFixed32:
		d.pos += 4
	default:
		return fmt.Errorf("未知线缆类型: %d", wireType)
	}
	return nil
}
// --- 字段兼容性：V1/V2 编解码器 ---
func encodeModelInfoV1(name, version string) []byte {
	enc := NewTLVEncoder()
	enc.WriteString(1, name)
	enc.WriteString(2, version)
	return enc.Bytes()
}
func encodeModelInfoV2(info *ModelInfo) []byte {
	enc := NewTLVEncoder()
	enc.WriteString(1, info.Name)
	enc.WriteString(2, info.Version)
	enc.WriteInt32(3, int32(info.Status))
	enc.WriteInt64(4, info.Parameters)
	for _, cap := range info.Capabilities {
		enc.WriteString(5, cap) // repeated: 同一字段编号重复出现
	}
	for k, v := range info.Labels {
		enc.WriteString(7, k+"="+v) // map 简化为 "key=value" 字符串
	}
	return enc.Bytes()
}

// decodeModelInfoV1Only V1 解码器 - 跳过不认识的 V2 字段（向前兼容）
func decodeModelInfoV1Only(data []byte) (name, version string, err error) {
	dec := NewTLVDecoder(data)
	for dec.HasMore() {
		fieldNum, wireType, e := dec.ReadTag()
		if e != nil {
			return "", "", e
		}
		switch fieldNum {
		case 1:
			name, err = dec.ReadString()
		case 2:
			version, err = dec.ReadString()
		default:
			err = dec.SkipField(wireType) // 跳过未知字段！
		}
		if err != nil {
			return "", "", err
		}
	}
	return
}
// --- 对象池化：sync.Pool 复用缓冲区减少 GC ---
var encoderPool = sync.Pool{
	New: func() interface{} { return &bytes.Buffer{} },
}
func encodeWithPool(info *ModelInfo) []byte {
	buf := encoderPool.Get().(*bytes.Buffer)
	defer func() { buf.Reset(); encoderPool.Put(buf) }()

	enc := NewTLVEncoderWithBuffer(buf)
	enc.WriteString(1, info.Name)
	enc.WriteString(2, info.Version)
	enc.WriteInt32(3, int32(info.Status))
	enc.WriteInt64(4, info.Parameters)
	for _, cap := range info.Capabilities {
		enc.WriteString(5, cap)
	}
	for k, v := range info.Labels {
		enc.WriteString(7, k+"="+v)
	}

	result := make([]byte, len(enc.Bytes()))
	copy(result, enc.Bytes()) // 必须复制，底层 buffer 会归还池中
	return result
}
// --- 主函数 ---
func main() {
	fmt.Println("=== Day 09: Protobuf 进阶实践 ===")
	fmt.Println()

	// 1. 枚举类型
	fmt.Println("--- 1. 枚举类型（Enum）---")
	fmt.Println("Protobuf 枚举要求第一个值为 0，用作默认值")
	fmt.Println()

	for _, s := range []ModelStatus{
		ModelStatusUnknown, ModelStatusLoading, ModelStatusReady,
		ModelStatusError, ModelStatusDeprecated,
	} {
		fmt.Printf("  值=%d  名称=%-28s\n", int32(s), s)
	}
	// proto3 支持未知枚举值，不会报错
	fmt.Printf("  值=99 名称=%-28s （未知值被保留）\n", ModelStatus(99))
	fmt.Println()

	// 2. oneof 字段
	fmt.Println("--- 2. oneof 字段（联合类型）---")
	fmt.Println("同一时刻只能设置一个字段，Go 中用 interface + 类型断言实现")
	fmt.Println()

	requests := []InferenceRequest{
		{
			RequestID: "req-001", ModelName: "gpt-4",
			Input:    &TextInput{Content: "翻译这段话", Language: "zh-CN"},
			Metadata: map[string]string{"priority": "high", "source": "api"},
			Tags:     []string{"translation", "chinese"},
		},
		{
			RequestID: "req-002", ModelName: "dall-e-3",
			Input:    &ImageInput{Data: []byte{0x89, 0x50, 0x4E, 0x47}, Format: "png", Width: 1024, Height: 768},
			Metadata: map[string]string{"style": "vivid"},
			Tags:     []string{"image", "generation"},
		},
		{
			RequestID: "req-003", ModelName: "whisper-v3",
			Input:    &AudioInput{Data: []byte{0xFF, 0xFB, 0x90}, SampleRate: 16000, Channels: 1},
			Metadata: map[string]string{"format": "mp3"},
			Tags:     []string{"transcription"},
		},
	}

	for _, req := range requests {
		fmt.Printf("  请求 %s (模型: %s)\n", req.RequestID, req.ModelName)
		// 类型断言 switch - Go 中处理 oneof 的标准方式
		switch input := req.Input.(type) {
		case *TextInput:
			fmt.Printf("    → 文本: %s (语言: %s)\n", input.Content, input.Language)
		case *ImageInput:
			fmt.Printf("    → 图片: %s %dx%d, %d字节\n", input.Format, input.Width, input.Height, len(input.Data))
		case *AudioInput:
			fmt.Printf("    → 音频: %dHz %d声道, %d字节\n", input.SampleRate, input.Channels, len(input.Data))
		}
		fmt.Printf("    元数据: %v  标签: %v\n", req.Metadata, req.Tags)
	}
	fmt.Println()

	// 3. 嵌套消息与 Map 类型
	fmt.Println("--- 3. 嵌套消息与 Map 类型 ---")
	info := &ModelInfo{
		Name:         "gpt-4-turbo",
		Version:      "2024.01",
		Status:       ModelStatusReady,
		Parameters:   1760000000000,
		Capabilities: []string{"chat", "code", "vision", "function_calling"},
		Labels:       map[string]string{"team": "openai", "tier": "premium", "region": "us-east"},
	}

	jsonData, _ := json.MarshalIndent(info, "  ", "  ")
	fmt.Printf("  模型信息 (JSON):\n  %s\n\n", string(jsonData))
	fmt.Printf("  嵌套访问: 状态=%s, 参数量=%.2f万亿\n", info.Status, float64(info.Parameters)/1e12)
	fmt.Printf("  Map 访问: team=%s, tier=%s\n", info.Labels["team"], info.Labels["tier"])
	fmt.Printf("  Repeated: capabilities=%v (共%d项)\n\n", info.Capabilities, len(info.Capabilities))

	// 4. 字段编号与兼容性
	fmt.Println("--- 4. 字段编号与向前/向后兼容性 ---")
	fmt.Println("  向后兼容: V2 解码器读 V1 数据（新字段取默认值）")
	fmt.Println("  向前兼容: V1 解码器读 V2 数据（跳过不认识的字段）")

	// 场景 A：V1 → V1
	v1Data := encodeModelInfoV1("gpt-4", "1.0")
	name, version, err := decodeModelInfoV1Only(v1Data)
	if err != nil {
		fmt.Printf("  解码失败: %v\n", err)
		return
	}
	fmt.Printf("  场景A [V1→V1]: name=%s, version=%s\n", name, version)

	// 场景 B：V2 → V1（向前兼容）
	v2Data := encodeModelInfoV2(info)
	name, version, err = decodeModelInfoV1Only(v2Data)
	if err != nil {
		fmt.Printf("  解码失败: %v\n", err)
		return
	}
	fmt.Printf("  场景B [V2→V1]: name=%s, version=%s （新字段被安全跳过）\n", name, version)
	fmt.Printf("  V1=%d字节  V2=%d字节\n\n", len(v1Data), len(v2Data))

	// 5. Repeated 字段
	fmt.Println("--- 5. Repeated 字段（列表语义）---")
	emptyReq := InferenceRequest{RequestID: "req-empty", ModelName: "test"}
	fmt.Printf("  空 repeated: tags=%v, len=%d （nil 和空 slice 在 proto3 中等价）\n",
		emptyReq.Tags, len(emptyReq.Tags))
	emptyReq.Tags = append(emptyReq.Tags, "tag1", "tag2", "tag3")
	fmt.Printf("  追加后:     tags=%v, len=%d\n", emptyReq.Tags, len(emptyReq.Tags))

	// repeated 消息类型
	fmt.Printf("  批量请求: 包含 %d 个子请求\n", len(requests))
	for i, r := range requests {
		fmt.Printf("    [%d] %s → %s (%s)\n", i, r.RequestID, r.ModelName, r.Input.TypeName())
	}
	fmt.Println()

	// 6. 编解码性能优化
	fmt.Println("--- 6. 编解码性能优化 ---")
	fmt.Println("策略: sync.Pool 复用缓冲区 + 预分配减少 GC")
	const iterations = 50000

	start := time.Now()
	for i := 0; i < iterations; i++ {
		_ = encodeModelInfoV2(info)
	}
	normalDur := time.Since(start)

	start = time.Now()
	for i := 0; i < iterations; i++ {
		_ = encodeWithPool(info)
	}
	pooledDur := time.Since(start)

	start = time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = json.Marshal(info)
	}
	jsonDur := time.Since(start)

	fmt.Printf("  编码 %d 次:\n", iterations)
	fmt.Printf("    JSON 编码:    %v\n", jsonDur)
	fmt.Printf("    TLV 普通:     %v\n", normalDur)
	fmt.Printf("    TLV 池化:     %v\n\n", pooledDur)

	v2Binary := encodeModelInfoV2(info)
	v2JSON, _ := json.Marshal(info)
	fmt.Printf("  编码大小: JSON=%d字节, TLV=%d字节, 节省=%.1f%%\n",
		len(v2JSON), len(v2Binary),
		float64(len(v2JSON)-len(v2Binary))/float64(len(v2JSON))*100)

	fmt.Println("  sync.Pool 原理:")
	fmt.Println("    Get() 取对象（空时调 New）→ 使用 → Put() 归还")
	fmt.Println("    GC 可回收池中对象，适合短生命周期的临时缓冲区")

	// 7. Varint 编码细节
	fmt.Println("--- 7. Varint 编码细节 ---")
	fmt.Println("每字节低 7 位存数据，最高位=1 表示后续还有字节:")
	for _, v := range []int32{1, 127, 128, 300, 100000} {
		enc := NewTLVEncoder()
		enc.writeVarint(uint64(v))
		fmt.Printf("  值=%-8d → ", v)
		for _, b := range enc.Bytes() {
			fmt.Printf("%08b ", b)
		}
		fmt.Printf("(%d 字节)\n", len(enc.Bytes()))
	}

	// 总结
	fmt.Println("\n=== 总结 ===")
	fmt.Println("1. oneof: 联合类型，同一时刻只设一个字段，Go 用接口实现")
	fmt.Println("2. 嵌套消息: 消息可包含其他消息，构建复杂结构")
	fmt.Println("3. Map: 键值对，键必须是整数/字符串，编码为 repeated")
	fmt.Println("4. 枚举: 第一个值必须为 0（默认值），Go 用 iota 模拟")
	fmt.Println("5. Repeated: 有序列表，空列表不占空间（proto3）")
	fmt.Println("6. 兼容性: 字段编号+线缆类型实现向前/向后兼容")
	fmt.Println("7. 性能: sync.Pool 复用缓冲区，Varint 压缩小整数")
	fmt.Println("\n最佳实践:")
	fmt.Println("  - 字段编号永不更改，删除字段用 reserved 标记")
	fmt.Println("  - 优先用小编号（1-15 只需 1 字节标签）")
	fmt.Println("  - 高频场景用 sync.Pool 复用编码器")
	fmt.Println("  - oneof 适合互斥输入（文本/图片/音频）")
}
