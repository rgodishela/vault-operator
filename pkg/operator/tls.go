package operator

import (
	"crypto/rsa"
	"crypto/x509"
	"fmt"

	"github.com/coreos-inc/vault-operator/pkg/spec"
	"github.com/coreos-inc/vault-operator/pkg/util/k8sutil"
	"github.com/coreos-inc/vault-operator/pkg/util/tlsutil"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	orgForTLSCert        = []string{"coreos.com"}
	defaultClusterDomain = "cluster.local"
)

// prepareTLSSecrets creates three etcd TLS secrets (client, server, peer) containing TLS assets.
// Currently we self-generate the CA, and use the self generated CA to sign all the TLS certs.
func (v *Vaults) prepareTLSSecrets(vr *spec.Vault) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("prepare TLS secrets failed: %v", err)
		}
	}()
	// TODO: optional user pass-in CA.
	caKey, caCrt, err := newCACert()
	if err != nil {
		return err
	}

	se, err := newEtcdClientTLSSecret(vr, caKey, caCrt)
	if err != nil {
		return err
	}
	_, err = v.kubecli.CoreV1().Secrets(vr.Namespace).Create(se)
	if err != nil {
		return err
	}

	se, err = newEtcdServerTLSSecret(vr, caKey, caCrt, defaultClusterDomain)
	if err != nil {
		return err
	}
	_, err = v.kubecli.CoreV1().Secrets(vr.Namespace).Create(se)
	if err != nil {
		return err
	}

	se, err = newEtcdPeerTLSSecret(vr, caKey, caCrt, defaultClusterDomain)
	if err != nil {
		return err
	}
	_, err = v.kubecli.CoreV1().Secrets(vr.Namespace).Create(se)
	if err != nil {
		return err
	}
	return nil
}

// newEtcdClientTLSSecret returns a secret containg etcd client TLS assets
func newEtcdClientTLSSecret(vr *spec.Vault, caKey *rsa.PrivateKey, caCrt *x509.Certificate) (*v1.Secret, error) {
	return newTLSSecret(vr, caKey, caCrt, "etcd client", k8sutil.EtcdClientTLSSecretName(vr.Name), nil,
		map[string]string{
			"key":  "etcd-client.key",
			"cert": "etcd-client.crt",
			"ca":   "etcd-client-ca.crt",
		})
}

// newEtcdServerTLSSecret returns a secret containg etcd server TLS assets
func newEtcdServerTLSSecret(vr *spec.Vault, caKey *rsa.PrivateKey, caCrt *x509.Certificate, clusterDomain string) (*v1.Secret, error) {
	return newTLSSecret(vr, caKey, caCrt, "etcd server", k8sutil.EtcdServerTLSSecretName(vr.Name),
		[]string{
			"localhost",
			fmt.Sprintf("*.%s.%s.svc.%s", k8sutil.EtcdNameForVault(vr.Name), vr.Namespace, clusterDomain),
			fmt.Sprintf("%s-client.%s.svc.%s", k8sutil.EtcdNameForVault(vr.Name), vr.Namespace, clusterDomain),
		},
		map[string]string{
			"key":  "server.key",
			"cert": "server.crt",
			"ca":   "server-ca.crt",
		})
}

// newEtcdPeerTLSSecret returns a secret containg etcd peer TLS assets
func newEtcdPeerTLSSecret(vr *spec.Vault, caKey *rsa.PrivateKey, caCrt *x509.Certificate, clusterDomain string) (*v1.Secret, error) {
	return newTLSSecret(vr, caKey, caCrt, "etcd peer", k8sutil.EtcdPeerTLSSecretName(vr.Name),
		[]string{
			fmt.Sprintf("*.%s.%s.svc.%s", k8sutil.EtcdNameForVault(vr.Name), vr.Namespace, clusterDomain),
		},
		map[string]string{
			"key":  "peer.key",
			"cert": "peer.crt",
			"ca":   "peer-ca.crt",
		})
}

// newTLSSecret is a common utility for creating a secret containing TLS assets.
func newTLSSecret(vr *spec.Vault, caKey *rsa.PrivateKey, caCrt *x509.Certificate, commonName, secretName string,
	addrs []string, fieldMap map[string]string) (*v1.Secret, error) {
	tc := tlsutil.CertConfig{
		CommonName:   commonName,
		Organization: orgForTLSCert,
		AltNames:     tlsutil.NewAltNames(addrs),
	}
	key, crt, err := newKeyAndCert(caCrt, caKey, tc)
	if err != nil {
		return nil, fmt.Errorf("new TLS secret failed: %v", err)
	}
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   secretName,
			Labels: k8sutil.LabelsForVault(vr.Name),
		},
		Data: map[string][]byte{
			fieldMap["key"]:  tlsutil.EncodePrivateKeyPEM(key),
			fieldMap["cert"]: tlsutil.EncodeCertificatePEM(crt),
			fieldMap["ca"]:   tlsutil.EncodeCertificatePEM(caCrt),
		},
	}
	return secret, nil
}

func newCACert() (*rsa.PrivateKey, *x509.Certificate, error) {
	key, err := tlsutil.NewPrivateKey()
	if err != nil {
		return nil, nil, err
	}

	config := tlsutil.CertConfig{
		CommonName:   "vault operator CA",
		Organization: orgForTLSCert,
	}

	cert, err := tlsutil.NewSelfSignedCACertificate(config, key)
	if err != nil {
		return nil, nil, err
	}

	return key, cert, err
}

func newKeyAndCert(caCert *x509.Certificate, caPrivKey *rsa.PrivateKey, config tlsutil.CertConfig) (*rsa.PrivateKey, *x509.Certificate, error) {
	key, err := tlsutil.NewPrivateKey()
	if err != nil {
		return nil, nil, err
	}
	// TODO: tlsutil.NewSignedCertificate()create certs for both client and server auth. We can limit it stricter.
	cert, err := tlsutil.NewSignedCertificate(config, key, caCert, caPrivKey)
	if err != nil {
		return nil, nil, err
	}
	return key, cert, nil
}
