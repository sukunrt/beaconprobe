package node

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/libp2p/go-libp2p"
	mplex "github.com/libp2p/go-libp2p-mplex"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
)

// NewHost creates a libp2p host with TCP+QUIC transports, noise security, yamux+mplex muxers.
// It returns the host and the secp256k1 private key (needed for discv5).
// If keyFile is non-empty, the key is loaded from (or saved to) that path.
// The key file format matches prysm: hex-encoded raw secp256k1 private key bytes.
func NewHost(tcpPort, quicPort uint, keyFile string) (host.Host, *ecdsa.PrivateKey, error) {
	privKey, err := loadOrGenerateKey(keyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("key: %w", err)
	}

	libp2pKey, err := crypto.UnmarshalSecp256k1PrivateKey(gcrypto.FromECDSA(privKey))
	if err != nil {
		return nil, nil, fmt.Errorf("convert key: %w", err)
	}

	h, err := libp2p.New(
		libp2p.Identity(libp2pKey),
		libp2p.ListenAddrStrings(
			fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", tcpPort),
			fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic-v1", quicPort),
		),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Transport(libp2pquic.NewTransport),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer(yamux.ID, yamux.DefaultTransport),
		libp2p.Muxer(mplex.ID, mplex.DefaultTransport),
		libp2p.DisableRelay(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create libp2p host: %w", err)
	}

	return h, privKey, nil
}

// loadOrGenerateKey loads a secp256k1 private key from keyFile,
// or generates a new one (saving it to keyFile if non-empty).
// Accepts both raw 32-byte and hex-encoded 64-byte key files.
func loadOrGenerateKey(keyFile string) (*ecdsa.PrivateKey, error) {
	if keyFile != "" {
		if src, err := os.ReadFile(keyFile); err == nil {
			if len(src) == 32 {
				return gcrypto.ToECDSA(src)
			}
			raw := make([]byte, hex.DecodedLen(len(src)))
			if _, err := hex.Decode(raw, src); err != nil {
				return nil, fmt.Errorf("decode key hex: %w", err)
			}
			return gcrypto.ToECDSA(raw)
		}
	}

	privKey, err := ecdsa.GenerateKey(gcrypto.S256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	if keyFile != "" {
		raw := gcrypto.FromECDSA(privKey)
		dst := make([]byte, hex.EncodedLen(len(raw)))
		hex.Encode(dst, raw)
		if err := os.WriteFile(keyFile, dst, 0600); err != nil {
			return nil, fmt.Errorf("save key: %w", err)
		}
	}
	return privKey, nil
}
