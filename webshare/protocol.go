package webshare

import "encoding/json"

func MarshalClientAction(action ClientAction) ([]byte, error) { return json.Marshal(action) }
func UnmarshalClientAction(raw []byte) (ClientAction, error) { var a ClientAction; err := json.Unmarshal(raw, &a); return a, err }
func MarshalServerEvent(event ServerEvent) ([]byte, error) { return json.Marshal(event) }
func UnmarshalServerEvent(raw []byte) (ServerEvent, error) { var e ServerEvent; err := json.Unmarshal(raw, &e); return e, err }
