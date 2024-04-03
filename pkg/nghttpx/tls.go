/*
Copyright 2015 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/**
 * Copyright 2016, Z Lab Corporation. All rights reserved.
 * Copyright 2017, nghttpx Ingress controller contributors
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package nghttpx

import (
	"bytes"
	"cmp"
	"context"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"k8s.io/klog/v2"
)

const (
	// tlsDir is the directory where TLS certificates and private keys are stored.
	tlsDir = "tls"
	// tlsTicketKeyDir is the directory where TLS ticket key files are stored.
	tlsTicketKeyDir = "tls-ticket-key"

	// TLSTicketKeySize is the length of TLS ticket key.  The default value is for AES-128-CBC encryption.
	TLSTicketKeySize = 48
	// MaxTLSTicketKeyNum is the maximum number of TLS ticket keys retained in a Secret.
	MaxTLSTicketKeyNum = 12
)

// CreateTLSKeyPath returns TLS private key file path.
func CreateTLSKeyPath(dir, name string) string {
	return filepath.Join(dir, tlsDir, name+".key")
}

// CreateTLSCertPath returns TLS certificate file path.
func CreateTLSCertPath(dir, name string) string {
	return filepath.Join(dir, tlsDir, name+".crt")
}

// CreateTLSOCSPRespPath returns TLS OCSP response file path.
func CreateTLSOCSPRespPath(dir, name string) string {
	return filepath.Join(dir, tlsDir, name+".ocsp-resp")
}

// CreateTLSCred creates TLSCred for given private key and certificate.  ocspResp is optional, and could be nil.
func CreateTLSCred(dir, name string, cert, key, ocspResp []byte) *TLSCred {
	keyChecksum := Checksum(key)
	certChecksum := Checksum(cert)

	c := &TLSCred{
		Name: name,
		Key: PrivateChecksumFile{
			Path:     CreateTLSKeyPath(dir, hex.EncodeToString(keyChecksum)),
			Content:  key,
			Checksum: keyChecksum,
		},
		Cert: ChecksumFile{
			Path:     CreateTLSCertPath(dir, hex.EncodeToString(certChecksum)),
			Content:  cert,
			Checksum: certChecksum,
		},
	}

	if len(ocspResp) > 0 {
		ocspRespChecksum := Checksum(ocspResp)

		c.OCSPResp = &ChecksumFile{
			Path:     CreateTLSOCSPRespPath(dir, hex.EncodeToString(ocspRespChecksum)),
			Content:  ocspResp,
			Checksum: ocspRespChecksum,
		}
	}

	return c
}

// writeTLSKeyCert writes TLS private keys and certificates to their files.
func writeTLSKeyCert(ingConfig *IngressConfig) error {
	if err := MkdirAll(filepath.Join(ingConfig.ConfDir, tlsDir)); err != nil {
		return fmt.Errorf("unable to create TLS directory: %w", err)
	}

	if ingConfig.DefaultTLSCred != nil {
		if err := writeTLSCred(ingConfig.DefaultTLSCred); err != nil {
			return err
		}
	}

	for _, tlsCred := range ingConfig.SubTLSCred {
		if err := writeTLSCred(tlsCred); err != nil {
			return err
		}
	}

	return nil
}

// writeTLSCred writes TLS private key, certificate, and optionally OCSP response to tlsCred in their files.
func writeTLSCred(tlsCred *TLSCred) error {
	if err := WriteFile(tlsCred.Key.Path, tlsCred.Key.Content); err != nil {
		return fmt.Errorf("unable to write TLS private key: %w", err)
	}

	if err := WriteFile(tlsCred.Cert.Path, tlsCred.Cert.Content); err != nil {
		return fmt.Errorf("unable to write TLS certificate: %w", err)
	}

	if tlsCred.OCSPResp != nil {
		if err := WriteFile(tlsCred.OCSPResp.Path, tlsCred.OCSPResp.Content); err != nil {
			return fmt.Errorf("unable to write TLS OCSP response: %w", err)
		}
	}

	return nil
}

// TLSCredShareSamePaths returns if a and b share the same Key.Path, Cert.path, and OCSPResp.Path.
func TLSCredShareSamePaths(a, b *TLSCred) bool {
	return TLSCredCompare(a, b) == 0
}

func TLSCredCompare(a, b *TLSCred) int {
	if c := cmp.Compare(a.Key.Path, b.Key.Path); c != 0 {
		return c
	}

	if c := cmp.Compare(a.Cert.Path, b.Cert.Path); c != 0 {
		return c
	}

	return cmp.Compare(a.OCSPResp.GetPath(), b.OCSPResp.GetPath())
}

// SortTLSCred sorts creds in ascending order of Key.Path, Cert.Path, and OCSPResp.Path.
func SortTLSCred(creds []*TLSCred) {
	slices.SortFunc(creds, TLSCredCompare)
}

// RemoveDuplicateTLSCred removes duplicates from creds, which share the same Key.Path, Cert.Path, and OCSPResp.Path.  It assumes that creds
// are sorted by SortTLSCred.
func RemoveDuplicateTLSCred(creds []*TLSCred) []*TLSCred {
	return slices.CompactFunc(creds, TLSCredShareSamePaths)
}

func ReadLeafCertificate(certPEM []byte) (*x509.Certificate, error) {
	certDER, err := readLeafCertificate(certPEM)
	if err != nil {
		return nil, err
	}

	return x509.ParseCertificate(certDER)
}

// VerifyCertificate verifies cert.
func VerifyCertificate(ctx context.Context, cert *x509.Certificate, currentTime time.Time) error {
	log := klog.FromContext(ctx)

	log.V(4).Info("Certificate signature algorithm", "algorithm", cert.SignatureAlgorithm)

	switch cert.SignatureAlgorithm {
	case x509.MD2WithRSA, x509.MD5WithRSA, x509.SHA1WithRSA, x509.DSAWithSHA1, x509.ECDSAWithSHA1:
		return fmt.Errorf("unsupported signature algorithm: %v", cert.SignatureAlgorithm)
	}

	log.V(4).Info("Certificate validity period", "notBefore", cert.NotBefore, "notAfter", cert.NotAfter)

	if currentTime.Before(cert.NotBefore) || currentTime.After(cert.NotAfter) {
		return errors.New("certificate has expired or is not valid at the given moment of time")
	}

	return nil
}

var errCertNotFound = errors.New("certificate not found")

// readLeafCertificate returns the first certificate found in certPEM.
func readLeafCertificate(certPEM []byte) ([]byte, error) {
	for {
		block, rest := pem.Decode(certPEM)
		if block == nil {
			return nil, errCertNotFound
		}

		if block.Type != "CERTIFICATE" {
			certPEM = rest
			continue
		}

		return block.Bytes, nil
	}
}

// NormalizePEM reads series of PEM encoded data and re-encode them in PEM format to remove anomalies.
func NormalizePEM(data []byte) ([]byte, error) {
	var dst bytes.Buffer

	for {
		p, rest := pem.Decode(data)
		if p == nil {
			break
		}

		data = rest

		if err := pem.Encode(&dst, p); err != nil {
			return nil, err
		}
	}

	return dst.Bytes(), nil
}

func NewTLSTicketKey() ([]byte, error) {
	key := make([]byte, TLSTicketKeySize)

	if err := GenerateCryptoKey(key, []byte("tls ticket key")); err != nil {
		return nil, err
	}

	return key, nil
}

func NewInitialTLSTicketKey() ([]byte, error) {
	keys := make([][]byte, 2)

	for i := range keys {
		var err error

		keys[i], err = NewTLSTicketKey()
		if err != nil {
			return nil, err
		}
	}

	return bytes.Join(keys, nil), nil
}

func VerifyTLSTicketKey(ticketKey []byte) error {
	// Requires at least 2 keys for stable key rotation.
	if len(ticketKey) < TLSTicketKeySize*2 || len(ticketKey)%TLSTicketKeySize != 0 {
		return errors.New("invalid TLS ticket key size")
	}

	return nil
}

func UpdateTLSTicketKey(ticketKey []byte) ([]byte, error) {
	return UpdateTLSTicketKeyFunc(ticketKey, NewTLSTicketKey)
}

// UpdateTLSTicketKeyFunc generates new key via newTLSTicketKeyFunc, and rotates keys, then returns new TLS ticket key.  This function
// assumes that VerifyTLSTicketKey was called against ticketKey and succeeded.
//
// ticketKey must include at least 2 keys.  New key is placed to the last.  Because the first key is used for encryption, new key is not
// used for encryption immediately.  It starts encrypting TLS ticket after the next rotation in order to ensure that all controllers see
// this key.  At most MaxTLSTicketKeyNum keys, including new key, are retained.  The oldest keys are discarded if the number of keys exceeds
// MaxTLSTicketKeyNum.
//
// The rotation works as follows:
//
// 1. Move the last key (which is the new key generated in the previous update) to the first.
// 2. Discard oldest keys if the number of keys exceeds MaxTLSTicketKeyNum - 1.
// 3. Generate new key and place it to the last.
func UpdateTLSTicketKeyFunc(ticketKey []byte, newTLSTicketKeyFunc func() ([]byte, error)) ([]byte, error) {
	newKey, err := newTLSTicketKeyFunc()
	if err != nil {
		return nil, err
	}

	var newTicketKeyLen int
	if len(ticketKey) < TLSTicketKeySize*MaxTLSTicketKeyNum {
		newTicketKeyLen = len(ticketKey) + TLSTicketKeySize
	} else {
		newTicketKeyLen = TLSTicketKeySize * MaxTLSTicketKeyNum
	}

	newTicketKey := make([]byte, newTicketKeyLen)

	// The last key is the next encryption key.
	copy(newTicketKey, ticketKey[len(ticketKey)-TLSTicketKeySize:])
	copy(newTicketKey[TLSTicketKeySize:], ticketKey[:newTicketKeyLen-TLSTicketKeySize*2])
	copy(newTicketKey[newTicketKeyLen-TLSTicketKeySize:], newKey)

	return newTicketKey, nil
}

// CreateTLSTicketKeyFiles creates TLS ticket key files.  This function assume that VerifyTLSTicketKey was called against ticketKey and
// succeeded.
func CreateTLSTicketKeyFiles(dir string, ticketKey []byte) []*PrivateChecksumFile {
	dir = filepath.Join(dir, tlsTicketKeyDir)

	files := make([]*PrivateChecksumFile, len(ticketKey)/TLSTicketKeySize)

	for i := range files {
		offset := TLSTicketKeySize * i
		key := ticketKey[offset : offset+TLSTicketKeySize]

		files[i] = &PrivateChecksumFile{
			Path:     filepath.Join(dir, "key-"+strconv.Itoa(i)),
			Content:  key,
			Checksum: Checksum(key),
		}
	}

	return files
}

func writeTLSTicketKeyFiles(ingConfig *IngressConfig) error {
	if len(ingConfig.TLSTicketKeyFiles) == 0 {
		return nil
	}

	if err := MkdirAll(filepath.Join(ingConfig.ConfDir, tlsTicketKeyDir)); err != nil {
		return fmt.Errorf("unable to create TLS ticket key directory: %w", err)
	}

	for _, f := range ingConfig.TLSTicketKeyFiles {
		if err := WriteFile(f.Path, f.Content); err != nil {
			return fmt.Errorf("unable to write TLS ticket key file: %w", err)
		}
	}

	return nil
}
