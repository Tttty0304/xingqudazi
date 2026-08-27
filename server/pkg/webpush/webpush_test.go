package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// decryptForTest 是 RFC8291 加密流程的逆向实现，只存在于测试文件中，模拟浏览器/推送
// 服务收到消息后的解密行为（生产代码里的 Server 端永远不需要解密，只需要加密发送）。
// 用来对 encrypt()/Send() 的真实输出做端到端正确性校验，而不是仅凭代码审查"看起来对"。
func decryptForTest(t *testing.T, body []byte, uaPriv *ecdh.PrivateKey, authSecret []byte) []byte {
	t.Helper()

	if len(body) < 16+4+1 {
		t.Fatalf("body too short: %d bytes", len(body))
	}
	salt := body[0:16]
	idlen := int(body[20])
	if len(body) < 21+idlen {
		t.Fatalf("body too short for keyid: %d bytes, idlen=%d", len(body), idlen)
	}
	asPubBytes := body[21 : 21+idlen]
	ciphertext := body[21+idlen:]

	curve := ecdh.P256()
	asPub, err := curve.NewPublicKey(asPubBytes)
	if err != nil {
		t.Fatalf("parse server ephemeral pubkey: %v", err)
	}
	ecdhSecret, err := uaPriv.ECDH(asPub)
	if err != nil {
		t.Fatalf("ecdh: %v", err)
	}

	uaPubBytes := uaPriv.PublicKey().Bytes()
	authInfo := bytes.Join([][]byte{[]byte("WebPush: info"), {0x00}, uaPubBytes, asPubBytes}, nil)
	prk1 := hmacSHA256(authSecret, ecdhSecret)
	ikm, err := hkdfExpand(prk1, authInfo, 32)
	if err != nil {
		t.Fatalf("hkdf expand ikm: %v", err)
	}

	prk2 := hmacSHA256(salt, ikm)
	cek, err := hkdfExpand(prk2, append([]byte("Content-Encoding: aes128gcm"), 0x00), 16)
	if err != nil {
		t.Fatalf("hkdf expand cek: %v", err)
	}
	nonce, err := hkdfExpand(prk2, append([]byte("Content-Encoding: nonce"), 0x00), 12)
	if err != nil {
		t.Fatalf("hkdf expand nonce: %v", err)
	}

	block, err := aes.NewCipher(cek)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("new gcm: %v", err)
	}
	plainWithDelimiter, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("gcm open (decrypt failed): %v", err)
	}
	if len(plainWithDelimiter) == 0 || plainWithDelimiter[len(plainWithDelimiter)-1] != 0x02 {
		t.Fatalf("missing 0x02 last-record delimiter, got tail=%x", plainWithDelimiter)
	}
	return plainWithDelimiter[:len(plainWithDelimiter)-1]
}

// TestSendEndToEndEncryption 是本包的核心正确性验证：模拟浏览器生成一对真实的 ECDH
// 订阅密钥 + auth secret，用本包的 Send() 加密发送到一个本地 httptest.Server（充当
// Push Service），Server 端捕获原始请求，用刚生成的浏览器私钥反向解密，断言明文与
// 发送前完全一致，且请求头（Content-Encoding/TTL/Authorization vapid）符合协议要求。
// 这是不依赖真实浏览器/FCM/Mozilla Push Service 也能给出的最强正确性证据。
func TestSendEndToEndEncryption(t *testing.T) {
	curve := ecdh.P256()
	uaPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate subscriber key: %v", err)
	}
	authSecret := make([]byte, 16)
	if _, err := rand.Read(authSecret); err != nil {
		t.Fatalf("generate auth secret: %v", err)
	}

	var capturedBody []byte
	var capturedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = body
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	vapid, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("generate vapid keys: %v", err)
	}

	sub := Subscription{
		Endpoint: server.URL + "/push/abc123",
		P256dh:   b64url.EncodeToString(uaPriv.PublicKey().Bytes()),
		Auth:     b64url.EncodeToString(authSecret),
	}
	plaintext := []byte(`{"title":"新的好友请求","body":"alice 想加你为好友"}`)

	resp, err := Send(context.Background(), SendOptions{
		Subscription: sub,
		Payload:      plaintext,
		VAPID:        vapid,
		Subject:      "mailto:admin@example.com",
		TTL:          24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected mock push service to return 201, got %d", resp.StatusCode)
	}

	if got := capturedHeaders.Get("Content-Encoding"); got != "aes128gcm" {
		t.Errorf("Content-Encoding = %q, want aes128gcm", got)
	}
	if got := capturedHeaders.Get("TTL"); got != "86400" {
		t.Errorf("TTL = %q, want 86400 (24h)", got)
	}
	auth := capturedHeaders.Get("Authorization")
	if !bytes.Contains([]byte(auth), []byte("vapid t=")) || !bytes.Contains([]byte(auth), []byte("k="+vapid.PublicKey)) {
		t.Errorf("Authorization header malformed or missing expected VAPID public key: %q", auth)
	}

	decrypted := decryptForTest(t, capturedBody, uaPriv, authSecret)
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted payload mismatch:\n  got:  %s\n  want: %s", decrypted, plaintext)
	}
}

// TestGenerateVAPIDKeys_ProducesDistinctValidKeys 覆盖密钥生成的基础正确性
// （长度、每次调用不重复）。
func TestGenerateVAPIDKeys_ProducesDistinctValidKeys(t *testing.T) {
	k1, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("GenerateVAPIDKeys() error: %v", err)
	}
	k2, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("GenerateVAPIDKeys() error: %v", err)
	}
	if k1.PublicKey == k2.PublicKey || k1.PrivateKey == k2.PrivateKey {
		t.Fatal("expected two independently generated VAPID key pairs to differ")
	}

	pubBytes, err := b64url.DecodeString(k1.PublicKey)
	if err != nil {
		t.Fatalf("decode public key: %v", err)
	}
	if len(pubBytes) != 65 {
		t.Fatalf("expected uncompressed P-256 public key to be 65 bytes, got %d", len(pubBytes))
	}
	privBytes, err := b64url.DecodeString(k1.PrivateKey)
	if err != nil {
		t.Fatalf("decode private key: %v", err)
	}
	if len(privBytes) != 32 {
		t.Fatalf("expected P-256 private scalar to be 32 bytes, got %d", len(privBytes))
	}
}
