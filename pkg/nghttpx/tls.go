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
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package nghttpx

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/golang/glog"

	"k8s.io/kubernetes/pkg/api"
)

func writeFile(path string, content []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("couldn't create file %v: %v", path, err)
	}

	defer f.Close()

	if _, err := f.Write(content); err != nil {
		return fmt.Errorf("couldn't write to file %v: %v", path, err)
	}

	return nil
}

// AddOrUpdateCertAndKey creates a key and certificate files with the
// specified name, and returns the path to key, and certificate files,
// and checksum of them concatenated.
func (ngx *Manager) AddOrUpdateCertAndKey(name string, cert, key []byte) (*TLSCred, error) {
	keyFileName := filepath.Join(tlsDirectory, fmt.Sprintf("%v.key", name))
	certFileName := filepath.Join(tlsDirectory, fmt.Sprintf("%v.crt", name))

	if err := writeFile(keyFileName, key); err != nil {
		return nil, err
	}

	if err := writeFile(certFileName, cert); err != nil {
		return nil, err
	}

	return &TLSCred{
		Cert:     certFileName,
		Key:      keyFileName,
		Checksum: TLSCertKeyChecksum(cert, key),
	}, nil
}

// TLSCertKeyChecksum returns checksum of cert and key in hex string.
func TLSCertKeyChecksum(cert []byte, key []byte) string {
	h := sha256.New()
	h.Write(cert)
	h.Write(key)
	return hex.EncodeToString(h.Sum(nil))
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
	for i, _ := range left {
		if pems[j].Key == left[i].Key {
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
func TLSCredPrefix(secret *api.Secret) string {
	return fmt.Sprintf("%v_%v", secret.Namespace, secret.Name)
}
