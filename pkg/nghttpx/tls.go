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
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"
)

const (
	// tlsDir is the directory where TLS certificates and private keys are stored.
	tlsDir = "tls"
)

//CreateTLSKeyPath returns TLS private key file path.
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
		OCSPResp: ChecksumFile{
			Path:     CreateTLSOCSPRespPath(dir, name),
			Content:  ocspResp,
			Checksum: Checksum(ocspResp),
		},
	}, nil
}

// writeTLSKeyCert writes TLS private keys and certificates to their files.
func writeTLSKeyCert(ingConfig *IngressConfig) error {
	if err := MkdirAll(filepath.Join(ingConfig.ConfDir, tlsDir)); err != nil {
		return fmt.Errorf("Couldn't create tls directory: %v", err)
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

	if len(tlsCred.OCSPResp.Content) > 0 {
		if err := WriteFile(tlsCred.OCSPResp.Path, tlsCred.OCSPResp.Content); err != nil {
			return fmt.Errorf("failed to write TLS OCSP response: %v", err)
		}
	}

	return nil
}

// commonNames checks if the certificate and key file are valid
// returning the result of the validation and the list of hostnames
// contained in the common name/s
func CommonNames(certBlob []byte) ([]string, error) {
	block, _ := pem.Decode(certBlob)
	if block == nil {
		return []string{}, fmt.Errorf("No valid PEM formatted block found from certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return []string{}, err
	}

	cn := []string{cert.Subject.CommonName}
	if len(cert.DNSNames) > 0 {
		cn = append(cn, cert.DNSNames...)
	}

	glog.V(2).Infof("DNS %v %v\n", cn, len(cn))
	return cn, nil
}

// checkPrivateKey checks if the key is valid.
func CheckPrivateKey(keyBlob []byte) error {
	block, _ := pem.Decode(keyBlob)
	if block == nil {
		return fmt.Errorf("No valid PEM formatted block found from private key")
	}

	if _, err := parsePrivateKey(block.Bytes); err != nil {
		return err
	}

	return nil
}

// parsePrivateKey parses private key given in der.  This function is
// copied from crypto/tls/tls.go and has been modified to suite to our
// need.  The origina code has the following copyright notice:
//
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
func parsePrivateKey(der []byte) (crypto.PrivateKey, error) {
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}

	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		switch key := key.(type) {
		case *rsa.PrivateKey, *ecdsa.PrivateKey:
			return key, nil
		default:
			return nil, errors.New("Found unknown private key type in PKCS#8 wrapping")
		}
	}

	if key, err := x509.ParseECPrivateKey(der); err == nil {
		return key, nil
	}

	return nil, errors.New("Failed to parse private key")
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
