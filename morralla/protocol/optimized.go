package protocol

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
)

// OptimizedHandler manages protocol optimization for different data sizes
type OptimizedHandler struct {
	BinaryThreshold int64 // File size threshold for binary protocol
}

// NewOptimizedHandler creates a new optimized protocol handler
func NewOptimizedHandler(binaryThreshold int64) *OptimizedHandler {
	return &OptimizedHandler{
		BinaryThreshold: binaryThreshold,
	}
}

// ProtocolType represents the type of protocol to use
type ProtocolType int

const (
	ProtocolJSON ProtocolType = iota
	ProtocolBinary
	ProtocolCompressed
)

// ResponseOptimization holds response optimization settings
type ResponseOptimization struct {
	Protocol    ProtocolType
	Compressed  bool
	Chunked     bool
	ChunkSize   int
}

// OptimizeResponse determines the best protocol and optimizations for a response
func (h *OptimizedHandler) OptimizeResponse(dataSize int64, contentType string) ResponseOptimization {
	opt := ResponseOptimization{
		Protocol:  ProtocolJSON,
		ChunkSize: 64 * 1024, // 64KB default chunk size
	}

	// Determine protocol based on size
	if dataSize > h.BinaryThreshold {
		opt.Protocol = ProtocolBinary
		opt.Chunked = true
	}

	// Enable compression for large text content
	if dataSize > 1024 && isTextContent(contentType) {
		opt.Compressed = true
		if dataSize > h.BinaryThreshold {
			opt.Protocol = ProtocolCompressed
		}
	}

	// Adjust chunk size based on data size
	if dataSize > 10*1024*1024 { // 10MB
		opt.ChunkSize = 1024 * 1024 // 1MB chunks
	} else if dataSize > 1024*1024 { // 1MB
		opt.ChunkSize = 256 * 1024 // 256KB chunks
	}

	return opt
}

// EncodeResponse encodes response data using the optimized protocol
func (h *OptimizedHandler) EncodeResponse(data []byte, opt ResponseOptimization) ([]byte, error) {
	switch opt.Protocol {
	case ProtocolJSON:
		return h.encodeJSON(data, opt.Compressed)
	case ProtocolBinary:
		return h.encodeBinary(data, opt.Compressed)
	case ProtocolCompressed:
		return h.encodeCompressed(data)
	default:
		return nil, fmt.Errorf("unsupported protocol type: %v", opt.Protocol)
	}
}

// encodeJSON encodes data as JSON (standard MCP format)
func (h *OptimizedHandler) encodeJSON(data []byte, compressed bool) ([]byte, error) {
	if compressed {
		compressed, err := h.compressData(data)
		if err != nil {
			return nil, fmt.Errorf("compression failed: %v", err)
		}
		data = compressed
	}

	// For now, return data as-is since we're working within MCP framework
	// In a full implementation, this would wrap in proper JSON MCP response
	return data, nil
}

// encodeBinary encodes data using custom binary protocol
func (h *OptimizedHandler) encodeBinary(data []byte, compressed bool) ([]byte, error) {
	var buf bytes.Buffer

	// Binary protocol header
	// Magic number (4 bytes): 0x4D435042 ("MCPB" - MCP Binary)
	magic := uint32(0x4D435042)
	if err := binary.Write(&buf, binary.LittleEndian, magic); err != nil {
		return nil, err
	}

	// Version (1 byte)
	version := uint8(1)
	if err := binary.Write(&buf, binary.LittleEndian, version); err != nil {
		return nil, err
	}

	// Flags (1 byte): bit 0 = compressed, bits 1-7 reserved
	flags := uint8(0)
	if compressed {
		flags |= 0x01
		var err error
		data, err = h.compressData(data)
		if err != nil {
			return nil, fmt.Errorf("compression failed: %v", err)
		}
	}
	if err := binary.Write(&buf, binary.LittleEndian, flags); err != nil {
		return nil, err
	}

	// Reserved (2 bytes)
	reserved := uint16(0)
	if err := binary.Write(&buf, binary.LittleEndian, reserved); err != nil {
		return nil, err
	}

	// Data length (8 bytes)
	dataLen := uint64(len(data))
	if err := binary.Write(&buf, binary.LittleEndian, dataLen); err != nil {
		return nil, err
	}

	// Data payload
	buf.Write(data)

	return buf.Bytes(), nil
}

// encodeCompressed encodes data with maximum compression
func (h *OptimizedHandler) encodeCompressed(data []byte) ([]byte, error) {
	return h.compressData(data)
}

// compressData compresses data using gzip
func (h *OptimizedHandler) compressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	
	// Use best compression for maximum space savings
	writer, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	
	if err := writer.Close(); err != nil {
		return nil, err
	}
	
	return buf.Bytes(), nil
}

// decompressData decompresses gzip data
func (h *OptimizedHandler) decompressData(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	
	return io.ReadAll(reader)
}

// DecodeResponse decodes response data from optimized protocol
func (h *OptimizedHandler) DecodeResponse(data []byte) ([]byte, ProtocolType, error) {
	// Check if it's binary protocol
	if len(data) >= 8 {
		magic := binary.LittleEndian.Uint32(data[:4])
		if magic == 0x4D435042 { // "MCPB"
			return h.decodeBinary(data)
		}
	}

	// Check if it's compressed (gzip magic number)
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		decompressed, err := h.decompressData(data)
		if err != nil {
			return nil, ProtocolJSON, fmt.Errorf("decompression failed: %v", err)
		}
		return decompressed, ProtocolCompressed, nil
	}

	// Default to JSON protocol
	return data, ProtocolJSON, nil
}

// decodeBinary decodes binary protocol data
func (h *OptimizedHandler) decodeBinary(data []byte) ([]byte, ProtocolType, error) {
	if len(data) < 16 {
		return nil, ProtocolBinary, fmt.Errorf("binary data too short")
	}

	reader := bytes.NewReader(data)

	// Skip magic number (already verified)
	reader.Seek(4, io.SeekStart)

	// Read version
	var version uint8
	if err := binary.Read(reader, binary.LittleEndian, &version); err != nil {
		return nil, ProtocolBinary, err
	}

	if version != 1 {
		return nil, ProtocolBinary, fmt.Errorf("unsupported binary protocol version: %d", version)
	}

	// Read flags
	var flags uint8
	if err := binary.Read(reader, binary.LittleEndian, &flags); err != nil {
		return nil, ProtocolBinary, err
	}

	compressed := (flags & 0x01) != 0

	// Skip reserved bytes
	reader.Seek(2, io.SeekCurrent)

	// Read data length
	var dataLen uint64
	if err := binary.Read(reader, binary.LittleEndian, &dataLen); err != nil {
		return nil, ProtocolBinary, err
	}

	// Read data payload
	payload := make([]byte, dataLen)
	if _, err := reader.Read(payload); err != nil {
		return nil, ProtocolBinary, err
	}

	// Decompress if needed
	if compressed {
		decompressed, err := h.decompressData(payload)
		if err != nil {
			return nil, ProtocolBinary, fmt.Errorf("binary decompression failed: %v", err)
		}
		payload = decompressed
	}

	return payload, ProtocolBinary, nil
}

// StreamChunks streams large data in optimized chunks
func (h *OptimizedHandler) StreamChunks(data []byte, chunkSize int, callback func(chunk []byte, isLast bool) error) error {
	if len(data) == 0 {
		return callback([]byte{}, true)
	}

	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}

		chunk := data[i:end]
		isLast := end == len(data)

		if err := callback(chunk, isLast); err != nil {
			return fmt.Errorf("chunk callback error: %v", err)
		}
	}

	return nil
}

// isTextContent determines if content type is text-based (good for compression)
func isTextContent(contentType string) bool {
	textTypes := []string{
		"text/",
		"application/json",
		"application/xml",
		"application/javascript",
		"application/typescript",
		"text/plain",
		"text/html",
		"text/css",
		"text/markdown",
	}

	for _, textType := range textTypes {
		if len(contentType) >= len(textType) && contentType[:len(textType)] == textType {
			return true
		}
	}

	return false
}

// GetCompressionRatio calculates compression ratio for given data
func (h *OptimizedHandler) GetCompressionRatio(original, compressed []byte) float64 {
	if len(original) == 0 {
		return 0.0
	}
	return float64(len(compressed)) / float64(len(original))
}

// ShouldUseCompression determines if compression would be beneficial
func (h *OptimizedHandler) ShouldUseCompression(data []byte, contentType string) bool {
	// Don't compress small data
	if len(data) < 1024 {
		return false
	}

	// Only compress text-based content
	if !isTextContent(contentType) {
		return false
	}

	// Test compression ratio with a sample
	if len(data) > 8192 {
		sample := data[:8192]
		compressed, err := h.compressData(sample)
		if err != nil {
			return false
		}

		// Only compress if we get at least 20% reduction
		ratio := h.GetCompressionRatio(sample, compressed)
		return ratio < 0.8
	}

	return true
}

// BenchmarkProtocol runs a quick benchmark to determine optimal protocol
func (h *OptimizedHandler) BenchmarkProtocol(data []byte) (ProtocolType, error) {
	dataSize := int64(len(data))
	
	// For very small data, always use JSON
	if dataSize < 1024 {
		return ProtocolJSON, nil
	}

	// For medium data, test compression
	if dataSize < h.BinaryThreshold {
		if h.ShouldUseCompression(data, "text/plain") {
			return ProtocolCompressed, nil
		}
		return ProtocolJSON, nil
	}

	// For large data, use binary protocol
	return ProtocolBinary, nil
}