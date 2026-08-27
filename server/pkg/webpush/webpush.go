// Package webpush 实现 Web Push 协议的最小必要子集：
//   - RFC 8291（Message Encryption for Web Push，aes128gcm content-encoding）
//   - RFC 8292（Voluntary Application Server Identification，VAPID）
//
// 用于 Task17（离线通知）：向浏览器 PushSubscription 端点发送加密推送消息。
// 仅实现"服务端发送"方向；解密在浏览器/Push Service 侧完成，本包不提供解密能力。
package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/hkdf"
)

// b64url 是 Web Push 生态统一使用的 base64url-without-padding 编码。
var b64url = base64.RawURLEncoding

// Subscription 对应浏览器 `PushManager.subscribe()` 返回的订阅信息
// （通过 Task9 前端 —— 尚未实现 —— 上报给后端；本包只消费其内容）。
type Subscription struct {
	Endpoint string
	P256dh   string // base64url，浏览器 ECDH 公钥（未压缩点，65字节）
	Auth     string // base64url，认证密钥（16字节）
}

// VAPIDKeys 是应用服务器级别的 ECDSA P-256 密钥对，用于对每次推送请求做身份声明
// （RFC8292），与每条消息加密时临时生成的 ECDH 密钥对是两套不同用途的密钥材料。
type VAPIDKeys struct {
	PublicKey  string // base64url，未压缩点（65字节）
	PrivateKey string // base64url，原始标量 D（32字节）
}

// GenerateVAPIDKeys 生成一对新的 VAPID 密钥。demo/评估项目在未配置环境变量时
// 于进程启动时自动生成一次（详见 config），代价是每次重启后浏览器旧订阅的
// VAPID 校验会失效——生产环境应固定配置，已在文档中如实标注。
func GenerateVAPIDKeys() (*VAPIDKeys, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate vapid key: %w", err)
	}
	// elliptic.Marshal 已在 Go 1.21 起废弃（Task11 lint 收尾时 staticcheck 命中），
	// 改用官方建议的 crypto/ecdh 路径：ecdh.PublicKey.Bytes() 输出与
	// elliptic.Marshal 完全等价的未压缩点格式（0x04 || X || Y），行为不变。
	ecdhPriv, err := priv.ECDH()
	if err != nil {
		return nil, fmt.Errorf("convert vapid key to ecdh: %w", err)
	}
	pub := ecdhPriv.PublicKey().Bytes()
	return &VAPIDKeys{
		PublicKey:  b64url.EncodeToString(pub),
		PrivateKey: b64url.EncodeToString(priv.D.FillBytes(make([]byte, 32))),
	}, nil
}

func (k *VAPIDKeys) ecdsaPrivateKey() (*ecdsa.PrivateKey, error) {
	dBytes, err := b64url.DecodeString(k.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("decode vapid private key: %w", err)
	}
	priv := new(ecdsa.PrivateKey)
	priv.PublicKey.Curve = elliptic.P256()
	priv.D = new(big.Int).SetBytes(dBytes)
	priv.PublicKey.X, priv.PublicKey.Y = priv.PublicKey.Curve.ScalarBaseMult(dBytes)
	return priv, nil
}

// vapidJWT 生成 RFC8292 要求的 VAPID JWT：aud 为推送端点的 scheme+host，
// exp 不超过 24 小时（这里固定 12 小时），sub 为可联系的标识（mailto/URL）。
func vapidJWT(endpoint, subject string, keys *VAPIDKeys) (string, error) {
	priv, err := keys.ecdsaPrivateKey()
	if err != nil {
		return "", err
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse endpoint url: %w", err)
	}
	aud := u.Scheme + "://" + u.Host

	claims := jwt.MapClaims{
		"aud": aud,
		"exp": time.Now().Add(12 * time.Hour).Unix(),
		"sub": subject,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	signed, err := token.SignedString(priv)
	if err != nil {
		return "", fmt.Errorf("sign vapid jwt: %w", err)
	}
	return signed, nil
}

// hmacSHA256 实现 HKDF-Extract 步骤（RFC5869 §2.2 等价于 HMAC(salt, IKM)），
// golang.org/x/crypto/hkdf 只导出 Expand，这里手动做 Extract 以便分两阶段
// 精确复现 RFC8291 §3.3/3.4 描述的两轮 HKDF 流程。
func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func hkdfExpand(prk, info []byte, length int) ([]byte, error) {
	out := make([]byte, length)
	if _, err := io.ReadFull(hkdf.Expand(sha256.New, prk, info), out); err != nil {
		return nil, err
	}
	return out, nil
}

// encryptedRecord 是 RFC8291 aes128gcm 编码后的完整消息体：
// header(salt+rs+idlen+keyid) || ciphertext，以及推送请求需要携带的服务端临时公钥。
type encryptedRecord struct {
	body []byte
}

// encrypt 按 RFC8291 §3.3/3.4 对 payload 加密，返回可直接作为 HTTP body 发送的字节流。
func encrypt(payload []byte, sub Subscription) (*encryptedRecord, error) {
	uaPubBytes, err := b64url.DecodeString(sub.P256dh)
	if err != nil {
		return nil, fmt.Errorf("decode subscriber public key: %w", err)
	}
	authSecret, err := b64url.DecodeString(sub.Auth)
	if err != nil {
		return nil, fmt.Errorf("decode subscriber auth secret: %w", err)
	}

	curve := ecdh.P256()
	uaPub, err := curve.NewPublicKey(uaPubBytes)
	if err != nil {
		return nil, fmt.Errorf("parse subscriber ecdh public key: %w", err)
	}

	// 每条消息生成一次性的应用服务器 ECDH 密钥对（RFC8291 术语 "as"），
	// 与 VAPID 身份密钥完全独立——前者只用于本条消息的密钥派生，不长期持有。
	asPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral ecdh key: %w", err)
	}
	asPub := asPriv.PublicKey()

	ecdhSecret, err := asPriv.ECDH(uaPub)
	if err != nil {
		return nil, fmt.Errorf("ecdh: %w", err)
	}

	// 第一阶段 HKDF（RFC8291 §3.3）：从 ECDH 共享密钥派生出 IKM。
	authInfo := bytes.Join([][]byte{
		[]byte("WebPush: info"), {0x00}, uaPubBytes, asPub.Bytes(),
	}, nil)
	prk1 := hmacSHA256(authSecret, ecdhSecret)
	ikm, err := hkdfExpand(prk1, authInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("hkdf expand ikm: %w", err)
	}

	// 第二阶段 HKDF（RFC8291 §3.4，结合本次随机 salt）：派生出 CEK 与 nonce。
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	prk2 := hmacSHA256(salt, ikm)
	cek, err := hkdfExpand(prk2, append([]byte("Content-Encoding: aes128gcm"), 0x00), 16)
	if err != nil {
		return nil, fmt.Errorf("hkdf expand cek: %w", err)
	}
	nonce, err := hkdfExpand(prk2, append([]byte("Content-Encoding: nonce"), 0x00), 12)
	if err != nil {
		return nil, fmt.Errorf("hkdf expand nonce: %w", err)
	}

	// 单记录消息：明文 + 0x02 分隔符（RFC8188 §2，标记"最后一条记录"，无需额外 padding）。
	record := append(append([]byte{}, payload...), 0x02)

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, fmt.Errorf("new aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, record, nil)

	// header：salt(16) || rs(4, big-endian 4096) || idlen(1, =65) || keyid(65)
	header := make([]byte, 0, 16+4+1+65)
	header = append(header, salt...)
	rs := make([]byte, 4)
	binary.BigEndian.PutUint32(rs, 4096)
	header = append(header, rs...)
	asPubBytes := asPub.Bytes()
	header = append(header, byte(len(asPubBytes)))
	header = append(header, asPubBytes...)

	body := append(header, ciphertext...)
	return &encryptedRecord{body: body}, nil
}

// SendOptions 携带一次推送请求所需的全部上下文。
type SendOptions struct {
	Subscription Subscription
	Payload      []byte // 明文通知内容（如 JSON `{"title":...,"body":...}`）
	VAPID        *VAPIDKeys
	Subject      string        // RFC8292 sub claim，如 "mailto:admin@example.com"
	TTL          time.Duration // 推送服务允许保留待送达消息的时长
}

// Send 向浏览器订阅端点发送一条加密推送消息（RFC8291 消息体 + RFC8292 VAPID 鉴权头）。
// 返回原始 *http.Response 供调用方判断推送服务的响应状态（201=已接受排队投递，
// 4xx 常见为订阅已失效，需要调用方据此清理失效订阅——本函数不做该项清理，由
// service 层按需处理）。
func Send(ctx context.Context, opts SendOptions) (*http.Response, error) {
	enc, err := encrypt(opts.Payload, opts.Subscription)
	if err != nil {
		return nil, fmt.Errorf("encrypt payload: %w", err)
	}

	jwtStr, err := vapidJWT(opts.Subscription.Endpoint, opts.Subject, opts.VAPID)
	if err != nil {
		return nil, fmt.Errorf("build vapid jwt: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.Subscription.Endpoint, bytes.NewReader(enc.body))
	if err != nil {
		return nil, fmt.Errorf("build push request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", fmt.Sprintf("%d", int(opts.TTL.Seconds())))
	req.Header.Set("Authorization", fmt.Sprintf("vapid t=%s, k=%s", jwtStr, opts.VAPID.PublicKey))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send push request: %w", err)
	}
	return resp, nil
}
