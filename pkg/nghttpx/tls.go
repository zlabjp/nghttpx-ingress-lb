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
	"context"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"k8s.io/klog/v2"
)

const (
	// tlsDir is the directory where TLS certificates and private keys are stored.
	tlsDir = "tls"
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
		return fmt.Errorf("failed to create TLS directory: %w", err)
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
		return fmt.Errorf("failed to write TLS private key: %w", err)
	}

	if err := WriteFile(tlsCred.Cert.Path, tlsCred.Cert.Content); err != nil {
		return fmt.Errorf("failed to write TLS certificate: %w", err)
	}

	if tlsCred.OCSPResp != nil {
		if err := WriteFile(tlsCred.OCSPResp.Path, tlsCred.OCSPResp.Content); err != nil {
			return fmt.Errorf("failed to write TLS OCSP response: %w", err)
		}
	}

	return nil
}

// PemsShareSamePaths returns if a and b share the same Key.Path, Cert.path, and OCSPResp.Path.
func PemsShareSamePaths(a, b *TLSCred) bool {
	return a.Key.Path == b.Key.Path && a.Cert.Path == b.Cert.Path &&
		((a.OCSPResp == nil && b.OCSPResp == nil) ||
			(a.OCSPResp != nil && b.OCSPResp != nil && a.OCSPResp.Path == b.OCSPResp.Path))
}

// SortPems sorts pems in ascending order of Key.Path, Cert.Path, and OCSPResp.Path.
func SortPems(pems []*TLSCred) {
	sort.Slice(pems, func(i, j int) bool {
		lhs, rhs := pems[i], pems[j]
		return lhs.Key.Path < rhs.Key.Path || (lhs.Key.Path == rhs.Key.Path && lhs.Cert.Path < rhs.Cert.Path) ||
			(lhs.Key.Path == rhs.Key.Path && lhs.Cert.Path == rhs.Cert.Path &&
				((lhs.OCSPResp == nil && rhs.OCSPResp != nil) ||
					(lhs.OCSPResp != nil && rhs.OCSPResp != nil && lhs.OCSPResp.Path < rhs.OCSPResp.Path)))
	})
}

// RemoveDuplicatePems removes duplicates from pems, which share the same Key.Path, Cert.Path, and OCSPResp.Path.  It assumes that pems are
// sorted by SortPems.
func RemoveDuplicatePems(pems []*TLSCred) []*TLSCred {
	if len(pems) == 0 {
		return pems
	}
	left := pems[1:]
	j := 0
	for i := range left {
		a := pems[j]
		b := left[i]

		if PemsShareSamePaths(a, b) {
			continue
		}
		j++
		if j <= i {
			pems[j] = left[i]
		}
	}
	return pems[:j+1]
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
