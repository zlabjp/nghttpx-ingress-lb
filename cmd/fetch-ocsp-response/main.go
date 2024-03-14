package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/ocsp"
)

var (
	timeout = time.Minute
)

func main() {
	rootCmd := &cobra.Command{
		Use:                   "fetch-ocsp-response <CERTIFICATE>",
		DisableFlagsInUseLine: true,
		Args:                  cobra.ExactArgs(1),
		Run:                   run,
	}

	rootCmd.Flags().DurationVar(&timeout, "timeout", timeout, "HTTP request timeout")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(255)
	}
}

func run(_ *cobra.Command, args []string) {
	certs, err := loadCertificates(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to load certificate: %v\n", err)
		os.Exit(255)
	}

	if len(certs) == 0 {
		fmt.Fprintf(os.Stderr, "No certificate found\n")
		os.Exit(255)
	}

	if len(certs[0].OCSPServer) == 0 {
		fmt.Fprintf(os.Stderr, "No OSCSP server was found\n")
		os.Exit(255)
	}

	var respDER []byte

	if len(certs) > 1 {
		respDER, err = getOCSPResponse(certs[0], certs[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to get OCSP response: %v\n", err)
			os.Exit(exitCode(err))
		}
	} else {
		chains, err := certs[0].Verify(x509.VerifyOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to verify certificate: %v\n", err)
			os.Exit(255)
		}

		for _, certs := range chains {
			if len(certs) < 2 {
				continue
			}

			respDER, err = getOCSPResponse(certs[0], certs[1])
			if err != nil {
				continue
			}

			break
		}

		if respDER == nil {
			if err == nil {
				err = errors.New("no issuer found")
			}

			fmt.Fprintf(os.Stderr, "Unable to get OCSP response: %v\n", err)
			os.Exit(exitCode(err))
		}
	}

	os.Stdout.Write(respDER)
}

// getOCSPResponse retrieves OCSP response from OCSP responder.  It returns the DER encoded response.
func getOCSPResponse(cert, issuer *x509.Certificate) ([]byte, error) {
	ocspReq, err := ocsp.CreateRequest(cert, issuer, nil)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cert.OCSPServer[0], bytes.NewBuffer(ocspReq))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/ocsp-request")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, newTemporaryError(err.Error())
	}

	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, newTemporaryError(err.Error())
	}

	if _, err := ocsp.ParseResponseForCert(respData, cert, issuer); err != nil {
		return nil, err
	}

	return respData, nil
}

// loadCertificates loads certificates from path.  The file must be PEM encoded.  The file must contain the leaf certificate first.  It can
// contain issuer certificate.
func loadCertificates(path string) ([]*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var certs []*x509.Certificate

	for {
		blk, rest := pem.Decode(data)
		if blk == nil {
			return certs, nil
		}

		data = rest

		if blk.Type != "CERTIFICATE" {
			continue
		}

		cert, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			return nil, err
		}

		certs = append(certs, cert)
	}
}

// temporaryError is a kind of error which indicates the temporary error.  It might be resolved if retried.
type temporaryError struct {
	msg string
}

func newTemporaryError(msg string) *temporaryError {
	return &temporaryError{
		msg: msg,
	}
}

func (err *temporaryError) Error() string {
	return err.msg
}

// exitCode return exist code for err.
func exitCode(err error) int {
	var tempErr *temporaryError
	if errors.As(err, &tempErr) {
		return 75
	}

	return 255
}
