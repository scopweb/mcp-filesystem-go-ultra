package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// Bound nesting and node allocation while parsing, rather than discovering an
// oversized tree only after encoding/json has allocated every map and slice.
type inspectJSONResourceError struct{ message string }

func (e *inspectJSONResourceError) Error() string { return e.message }

func inspectDecodeJSON(ctx context.Context, decoder *json.Decoder, depth int, nodes *int) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	*nodes++
	if *nodes > inspectJSONNodes || depth > inspectJSONDepth {
		return nil, &inspectJSONResourceError{fmt.Sprintf("JSON exceeds %d nodes or depth %d", inspectJSONNodes, inspectJSONDepth)}
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		count := 0
		value, err := inspectJSONNumbers(ctx, token, 0, &count)
		if err != nil {
			return nil, &inspectJSONResourceError{err.Error()}
		}
		return value, nil
	}
	switch delimiter {
	case '[':
		values := make([]any, 0)
		for decoder.More() {
			value, err := inspectDecodeJSON(ctx, decoder, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		close, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if close != json.Delim(']') {
			return nil, fmt.Errorf("expected closing array bracket")
		}
		return values, nil
	case '{':
		object := map[string]any{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("expected object key")
			}
			value, err := inspectDecodeJSON(ctx, decoder, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			object[name] = value
		}
		close, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if close != json.Delim('}') {
			return nil, fmt.Errorf("expected closing object brace")
		}
		return object, nil
	default:
		return nil, fmt.Errorf("unexpected JSON delimiter")
	}
}

func inspectJSONEnd(decoder *json.Decoder) error {
	_, err := decoder.Token()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("expected a single JSON document")
}
