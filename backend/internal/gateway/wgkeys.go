package gateway

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"time"
)

type wgKeyMaterial struct {
	Name    string
	Private []byte
	Public  string
	Expires time.Time
}

func (a *App) newWGKey(name string) (M, error) {
	if e := iface(name); e != nil {
		return nil, e
	}
	if _, e := os.Lstat("/etc/wireguard/" + name + ".conf"); !os.IsNotExist(e) {
		return nil, errors.New("同名接口已存在，新增密钥不会覆盖它")
	}
	private := make([]byte, 32)
	if _, e := rand.Read(private); e != nil {
		return nil, e
	}
	private[0] &= 248
	private[31] &= 127
	private[31] |= 64
	key, e := ecdh.X25519().NewPrivateKey(private)
	if e != nil {
		return nil, e
	}
	public := base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
	identity := id()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.wgKeys == nil {
		a.wgKeys = map[string]wgKeyMaterial{}
	}
	for token, value := range a.wgKeys {
		if time.Now().After(value.Expires) || value.Name == name {
			for i := range value.Private {
				value.Private[i] = 0
			}
			delete(a.wgKeys, token)
		}
	}
	if len(a.wgKeys) >= 64 {
		return nil, errors.New("临时密钥数量已达上限，请稍后再试")
	}
	expires := time.Now().Add(10 * time.Minute)
	a.wgKeys[identity] = wgKeyMaterial{Name: name, Private: private, Public: public, Expires: expires}
	return M{"key_id": identity, "public_key": public, "expires": float64(expires.Unix()), "algorithm": "Curve25519", "private_key_status": "仅保存在服务器内存，应用接口配置时写入受限文件"}, nil
}
func (a *App) consumeWGKey(token, name string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	value, ok := a.wgKeys[token]
	if !ok || value.Name != name || time.Now().After(value.Expires) {
		return "", errors.New("密钥预览已过期或接口名已改变，请重新生成")
	}
	return base64.StdEncoding.EncodeToString(value.Private), nil
}
func (a *App) forgetWGKey(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	value := a.wgKeys[token]
	for i := range value.Private {
		value.Private[i] = 0
	}
	delete(a.wgKeys, token)
}
func publicWGKey(private string) string {
	bytes, e := base64.StdEncoding.DecodeString(private)
	if e != nil || len(bytes) != 32 {
		return ""
	}
	key, e := ecdh.X25519().NewPrivateKey(bytes)
	if e != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
}
