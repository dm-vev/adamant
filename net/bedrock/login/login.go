// Package login encodes the login request of a client, the connection
// request of its Login packet, and reads it for a server: a multiplayer
// token naming the player and the public key of the client, and the data
// of its device and skin, each a JWT signed with its P-384 key. The
// authorization service of the game signs the token of a player of Xbox
// Live; a client without an account signs it itself, which a server takes
// when it does not ask for Xbox Live.
package login

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Identity names a player.
type Identity struct {
	// DisplayName is the name of the player.
	DisplayName string
	// Identity is the UUID of the player.
	Identity string
	// XUID is the Xbox Live user ID, empty offline.
	XUID string
	// PlayFabID and PlayFabTitleID name the PlayFab account of the player.
	PlayFabID, PlayFabTitleID string
	// TitleID is the Xbox Live title of the game the player plays, which
	// only the certificate chain of a login names.
	TitleID string
}

// ClientData describes the device and the skin of a client, under the
// names the game gives them in JSON.
type ClientData struct {
	AnimatedImageData                []SkinAnimation
	ArmSize                          string
	CapeData                         string
	CapeID                           string `json:"CapeId"`
	CapeImageHeight                  int
	CapeImageWidth                   int
	CapeOnClassicSkin                bool
	ClientEditorConnectionIntent     int
	ClientIsEditorCapable            bool
	ClientRandomID                   int64 `json:"ClientRandomId"`
	CompatibleWithClientSideChunkGen bool
	CurrentInputMode                 int
	DefaultInputMode                 int
	DeviceID                         string `json:"DeviceId"`
	DeviceModel                      string
	DeviceOS                         int
	FilterProfanity                  bool
	GameVersion                      string
	GraphicsMode                     int
	GUIScale                         int `json:"GuiScale"`
	LanguageCode                     string
	MaxViewDistance                  int
	MemoryTier                       int
	OverrideSkin                     bool
	PartyID                          string `json:"PartyId"`
	PartyLeader                      bool   `json:"IsPartyLeader"`
	PersonaPieces                    []PersonaPiece
	PersonaSkin                      bool
	PieceTintColours                 []PieceTintColour `json:"PieceTintColors"`
	PlatformOfflineID                string            `json:"PlatformOfflineId"`
	PlatformOnlineID                 string            `json:"PlatformOnlineId"`
	PlatformType                     int
	PlayFabID                        string `json:"PlayFabId"`
	PremiumSkin                      bool
	ProfileHash                      string
	SelfSignedID                     string `json:"SelfSignedId"`
	ServerAddress                    string
	SkinAnimationData                string
	SkinColour                       string `json:"SkinColor"`
	SkinData                         string
	SkinGeometry                     string `json:"SkinGeometryData"`
	SkinGeometryVersion              string `json:"SkinGeometryDataEngineVersion"`
	SkinID                           string `json:"SkinId"`
	SkinImageHeight                  int
	SkinImageWidth                   int
	SkinResourcePatch                string
	ThirdPartyName                   string
	TrustedSkin                      bool
	UIProfile                        int
}

// SkinAnimation is an animated layer of a skin.
type SkinAnimation struct {
	AnimationExpression int
	Frames              float64
	Image               string
	ImageHeight         int
	ImageWidth          int
	Type                int
}

// PersonaPiece is a piece of a persona skin.
type PersonaPiece struct {
	Default   bool   `json:"IsDefault"`
	PackID    string `json:"PackId"`
	PieceID   string `json:"PieceId"`
	PieceType string
	ProductID string `json:"ProductId"`
}

// PieceTintColour holds the colours of a piece of a persona skin.
type PieceTintColour struct {
	Colours   [4]string `json:"Colors"`
	PieceType string
}

// Devices and input modes of client data.
const (
	DeviceAndroid  = 1
	InputModeTouch = 2
)

// DefaultClientData returns the client data of an Android device with a
// plain skin, version being the version of the game it claims to run.
func DefaultClientData(serverAddress, name, version string) ClientData {
	device := make([]byte, 16)
	playFab := make([]byte, 8)
	_, _ = rand.Read(device)
	_, _ = rand.Read(playFab)
	var random [8]byte
	_, _ = rand.Read(random[:])
	return ClientData{
		AnimatedImageData: []SkinAnimation{},
		ClientRandomID:    int64(binary.LittleEndian.Uint64(random[:]) >> 1),
		CurrentInputMode:  InputModeTouch,
		DefaultInputMode:  InputModeTouch,
		// Android names a device by 32 lowercase hex digits.
		DeviceID:         hex.EncodeToString(device),
		DeviceOS:         DeviceAndroid,
		GameVersion:      version,
		LanguageCode:     "en_GB",
		PersonaPieces:    []PersonaPiece{},
		PieceTintColours: []PieceTintColour{},
		PlayFabID:        hex.EncodeToString(playFab),
		SelfSignedID:     NewUUID(),
		ServerAddress:    serverAddress,
		// A black skin of 64 by 32 pixels on the humanoid geometry the game
		// has built in.
		SkinData:            base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0, 0, 0, 255}, 64*32)),
		SkinGeometry:        base64.StdEncoding.EncodeToString([]byte("{}")),
		SkinGeometryVersion: base64.StdEncoding.EncodeToString([]byte("0.0.0")),
		SkinID:              NewUUID(),
		SkinImageHeight:     32,
		SkinImageWidth:      64,
		SkinResourcePatch:   base64.StdEncoding.EncodeToString([]byte(`{"geometry":{"default":"geometry.humanoid.custom"}}`)),
		ThirdPartyName:      name,
	}
}

// NewUUID returns a random UUID of version 4.
func NewUUID() string {
	var u [16]byte
	_, _ = rand.Read(u[:])
	u[6] = u[6]&0x0f | 0x40
	u[8] = u[8]&0x3f | 0x80
	h := hex.EncodeToString(u[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// multiplayerAudience is the audience of multiplayer tokens.
const multiplayerAudience = "api://auth-minecraft-services/multiplayer"

// tokenClaims are the claims of a multiplayer token.
type tokenClaims struct {
	Expiry               int64  `json:"exp"`
	NotBefore            int64  `json:"nbf"`
	Audience             string `json:"aud"`
	IdentityProviderType string `json:"ipt"`
	PlayFabID            string `json:"mid"`
	PlayFabTitleID       string `json:"tid"`
	ClientPublicKey      string `json:"cpk"`
	XUID                 string `json:"xid"`
	DisplayName          string `json:"xname"`
	Identity             string `json:"leguuid,omitempty"`
}

// request is the JSON that opens a connection request: the certificate
// chain, empty for a multiplayer token, as a JSON string of its own.
type request struct {
	Certificate        string
	AuthenticationType uint8
	Token              string
}

// selfSigned is the authentication type of a self-signed token.
const selfSigned = 2

// EncodeOffline returns the connection request of a client that signs its
// multiplayer token with key itself, valid from six hours before now to
// six hours after.
func EncodeOffline(id Identity, data ClientData, key *ecdsa.PrivateKey, now time.Time) ([]byte, error) {
	pub, err := MarshalPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	token, err := sign(key, pub, tokenClaims{
		Expiry:          now.Add(6 * time.Hour).Unix(),
		NotBefore:       now.Add(-6 * time.Hour).Unix(),
		Audience:        multiplayerAudience,
		PlayFabID:       id.PlayFabID,
		PlayFabTitleID:  id.PlayFabTitleID,
		ClientPublicKey: pub,
		XUID:            id.XUID,
		DisplayName:     id.DisplayName,
		Identity:        id.Identity,
	})
	if err != nil {
		return nil, err
	}
	return encode(request{Certificate: `{"chain":[""]}`, AuthenticationType: selfSigned, Token: token}, data, key, pub)
}

// full is the authentication type of a token of the authorization service.
const full = 0

// EncodeToken returns the connection request of a client that logs in
// with a multiplayer token the authorization service issued for the public
// key of key, as a player of Xbox Live: the client data is signed with key.
func EncodeToken(token string, data ClientData, key *ecdsa.PrivateKey) ([]byte, error) {
	pub, err := MarshalPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	return encode(request{Certificate: `{"chain":[""]}`, AuthenticationType: full, Token: token}, data, key, pub)
}

// encode returns the connection request of req with the client data
// signed with key.
func encode(req request, data ClientData, key *ecdsa.PrivateKey, pub string) ([]byte, error) {
	clientData, err := sign(key, pub, data)
	if err != nil {
		return nil, err
	}
	head, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	// Each part goes with its length as a little-endian int32.
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(head)))
	b = append(b, head...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(clientData)))
	return append(b, clientData...), nil
}

// HandshakeToken returns the token of a ServerToClientHandshake: a salt
// signed by the key of the server, which the client takes, with the key,
// to derive the key that encrypts the connection.
func HandshakeToken(key *ecdsa.PrivateKey, salt []byte) (string, error) {
	pub, err := MarshalPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	return sign(key, pub, struct {
		Salt string `json:"salt"`
	}{base64.RawStdEncoding.EncodeToString(salt)})
}

// MarshalPublicKey encodes a public key as the login does: base64 of its
// DER SubjectPublicKeyInfo.
func MarshalPublicKey(key *ecdsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return "", fmt.Errorf("login: marshal public key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(der), nil
}

var b64 = base64.RawURLEncoding

// sign returns the compact JWS of claims signed with ES384, its header
// naming the public key in x5u.
func sign(key *ecdsa.PrivateKey, pub string, claims any) (string, error) {
	header, err := json.Marshal(struct {
		Alg string `json:"alg"`
		X5U string `json:"x5u"`
	}{"ES384", pub})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signed := b64.EncodeToString(header) + "." + b64.EncodeToString(payload)
	sum := sha512.Sum384([]byte(signed))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		return "", fmt.Errorf("login: sign: %w", err)
	}
	// JWS takes r and s as they are, each of 48 bytes.
	sig := make([]byte, 96)
	r.FillBytes(sig[:48])
	s.FillBytes(sig[48:])
	return signed + "." + b64.EncodeToString(sig), nil
}
