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
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"k8s.io/klog/v2"
	"path/filepath"

	"k8s.io/api/core/v1"
)

const (
	// tlsDir is the directory where TLS certificates and private keys are stored.
	tlsDir = "tls"
)

// CreateTLSKeyPath returns TLS private key file path.
func CreateTLSKeyPath(dir, name string) string {
	return filepath.Join(dir, tlsDir, fmt.Sprintf("%v.key", name))
}

// CreateTLSCertPath returns TLS certificate file path.
func CreateTLSCertPath(dir, name string) string {
	return filepath.Join(dir, tlsDir, fmt.Sprintf("%v.crt", name))
}

// CreateTLSOCSPRespPath returns TLS OCSP response file path.
func CreateTLSOCSPRespPath(dir, name string) string {
	return filepath.Join(dir, tlsDir, fmt.Sprintf("%v.ocsp-resp", name))
}

// CreateTLSCred creates TLSCred for given private key and certificate.  ocspResp is optional, and could be nil.
func CreateTLSCred(dir, name string, cert, key, ocspResp []byte) (*TLSCred, error) {
	return &TLSCred{
		Key: ChecksumFile{
			Path:     CreateTLSKeyPath(dir, name),
			Content:  key,
			Checksum: Checksum(key),
		},
		Cert: ChecksumFile{
			Path:     CreateTLSCertPath(dir, name),
			Content:  cert,
			Checksum: Checksum(cert),
		},
		OCSPResp: &ChecksumFile{
			Path:     CreateTLSOCSPRespPath(dir, name),
			Content:  ocspResp,
			Checksum: Checksum(ocspResp),
		},
	}, nil
}

// writeTLSKeyCert writes TLS private keys and certificates to their files.
func writeTLSKeyCert(ingConfig *IngressConfig) error {
	if err := MkdirAll(filepath.Join(ingConfig.ConfDir, tlsDir)); err != nil {
		return fmt.Errorf("failed to create tls directory: %v", err)
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
		return fmt.Errorf("failed to write TLS private key: %v", err)
	}

	if err := WriteFile(tlsCred.Cert.Path, tlsCred.Cert.Content); err != nil {
		return fmt.Errorf("failed to write TLS certificate: %v", err)
	}

	if tlsCred.OCSPResp != nil {
		if err := WriteFile(tlsCred.OCSPResp.Path, tlsCred.OCSPResp.Content); err != nil {
			return fmt.Errorf("failed to write TLS OCSP response: %v", err)
		}
	}

	return nil
}

// RemoveDuplicatePems removes duplicates from pems.  It assumes that pems are sorted using TLSCredKeyLess.
func RemoveDuplicatePems(pems []*TLSCred) []*TLSCred {
	if len(pems) == 0 {
		return pems
	}
	left := pems[1:]
	j := 0
	for i := range left {
		if pems[j].Key.Path == left[i].Key.Path {
			continue
		}
		j++
		if j <= i {
			pems[j] = left[i]
		}
	}
	return pems[:j+1]
}

// TLSCredPrefix returns prefix of TLS certificate/private key files.
func TLSCredPrefix(secret *v1.Secret) string {
	return fmt.Sprintf("%v_%v", secret.Namespace, secret.Name)
}

// VerifyCertificate verifies certPEM passed in PEM format.
func VerifyCertificate(certPEM []byte) error {
	certDER, err := readLeafCertificate(certPEM)
	if err != nil {
		return err
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return err
	}

	klog.V(4).Infof("Signature algorithm is %v", cert.SignatureAlgorithm)

	switch cert.SignatureAlgorithm {
	case x509.MD2WithRSA, x509.MD5WithRSA, x509.SHA1WithRSA, x509.DSAWithSHA1, x509.ECDSAWithSHA1:
		return fmt.Errorf("unsupported signature algorithm: %v", cert.SignatureAlgorithm)
	}

	return nil
}

// readLeafCertificate returns the first certificate found in certPEM.
func readLeafCertificate(certPEM []byte) ([]byte, error) {
	for {
		block, rest := pem.Decode(certPEM)
		if block == nil {
			return nil, errors.New("certificate not found")
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
