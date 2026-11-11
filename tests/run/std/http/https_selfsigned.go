package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"time"
)

func main() {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA: true, BasicConstraintsValid: true, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		fmt.Println(err)
		return
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
	if err != nil {
		fmt.Println(err)
		return
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "secure=%v proto=%s", r.TLS != nil, r.Proto)
	}), ErrorLog: log.New(io.Discard, "", 0)}
	go srv.Serve(ln)
	defer srv.Close()
	good := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	resp, err := good.Get("https://" + ln.Addr().String() + "/")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	b, _ := io.ReadAll(resp.Body)
	fmt.Println(resp.StatusCode, string(b), resp.TLS.Version == tls.VersionTLS13, resp.TLS.HandshakeComplete, resp.TLS.PeerCertificates[0].Subject.CommonName)
	bad := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{}}}
	_, err = bad.Get("https://" + ln.Addr().String() + "/")
	_, isUnknown := err.(interface{ Unwrap() error })
	fmt.Println(err != nil, isUnknown)
	skip := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	r2, err := skip.Get("https://" + ln.Addr().String() + "/")
	fmt.Println(err, r2.StatusCode)
}
