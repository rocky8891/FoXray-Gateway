package main

import (
	"encoding/json"
	"github.com/rocky8891/FoXray-Gateway/backend/internal/gateway"
	"io"
	"os"
)

func main() {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1048577))
	input := gateway.M{}
	if err != nil || len(raw) > 1048576 || json.Unmarshal(raw, &input) != nil {
		json.NewEncoder(os.Stdout).Encode(gateway.M{"error": "预览请求无效"})
		return
	}
	request, _ := input["request"].(map[string]any)
	snapshot, _ := input["snapshot"].(map[string]any)
	result, err := gateway.InterfacePreviewFromSnapshot(request, snapshot)
	if err != nil {
		json.NewEncoder(os.Stdout).Encode(gateway.M{"error": err.Error()})
		return
	}
	json.NewEncoder(os.Stdout).Encode(result)
}
