package dsig

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"

	"github.com/beevik/etree"
	"github.com/russellhaering/goxmldsig/etreeutils"
	"github.com/russellhaering/goxmldsig/types"
)

var uriRegexp = regexp.MustCompile("^#[a-zA-Z_][\\w.-]*$")
var whiteSpace = regexp.MustCompile("\\s+")

var (
	// ErrMissingSignature indicates that no enveloped signature was found referencing
	// the top level element passed for signature verification.
	ErrMissingSignature = errors.New("Missing signature referencing the top-level element")
)

type ValidationContext struct {
	CertificateStore X509CertificateStore
	IdAttribute      string
	Clock            *Clock
}

func NewDefaultValidationContext(certificateStore X509CertificateStore) *ValidationContext {
	return &ValidationContext{
		CertificateStore: certificateStore,
		IdAttribute:      DefaultIdAttr,
	}
}

// TODO(russell_h): More flexible namespace support. This might barely work.
func inNamespace(el *etree.Element, ns string) bool {
	for _, attr := range el.Attr {
		if attr.Value == ns {
			if attr.Space == "" && attr.Key == "xmlns" {
				return el.Space == ""
			} else if attr.Space == "xmlns" {
				return el.Space == attr.Key
			}
		}
	}

	return false
}

func childPath(space, tag string) string {
	if space == "" {
		return "./" + tag
	} else {
		return "./" + space + ":" + tag
	}
}

// The RemoveElement method on etree.Element isn't recursive...
func recursivelyRemoveElement(tree, el *etree.Element) bool {
	if tree.RemoveChild(el) != nil {
		return true
	}

	for _, child := range tree.Child {
		if childElement, ok := child.(*etree.Element); ok {
			if recursivelyRemoveElement(childElement, el) {
				return true
			}
		}
	}

	return false
}

// transform applies the passed set of transforms to the specified root element.
//
// The functionality of transform is currently very limited and purpose-specific.
//
// NOTE(russell_h): Ideally this wouldn't mutate the root passed to it, and would
// instead return a copy. Unfortunately copying the tree makes it difficult to
// correctly locate the signature. I'm opting, for now, to simply mutate the root
// parameter.
func (ctx *ValidationContext) transform(
	el *etree.Element,
	sig *types.Signature,
	ref *types.Reference) (*etree.Element, Canonicalizer, error) {
	transforms := ref.Transforms.Transforms

	if len(transforms) != 2 {
		return nil, nil, errors.New("Expected Enveloped and C14N transforms")
	}

	var canonicalizer Canonicalizer

	for _, transform := range transforms {
		algo := transform.Algorithm

		switch AlgorithmID(algo) {
		case EnvelopedSignatureAltorithmId:
			if !recursivelyRemoveElement(el, sig.UnderlyingElement()) {
				return nil, nil, errors.New("Error applying canonicalization transform: Signature not found")
			}

		case CanonicalXML10ExclusiveAlgorithmId:
			var prefixList string
			if transform.InclusiveNamespaces != nil {
				prefixList = transform.InclusiveNamespaces.PrefixList
			}

			canonicalizer = MakeC14N10ExclusiveCanonicalizerWithPrefixList(prefixList)

		case CanonicalXML11AlgorithmId:
			canonicalizer = MakeC14N11Canonicalizer()

		default:
			return nil, nil, errors.New("Unknown Transform Algorithm: " + algo)
		}
	}

	if canonicalizer == nil {
		return nil, nil, errors.New("Expected canonicalization transform")
	}

	return el, canonicalizer, nil
}

func (ctx *ValidationContext) digest(el *etree.Element, digestAlgorithmId string, canonicalizer Canonicalizer) ([]byte, error) {
	data, err := canonicalizer.Canonicalize(el)
	if err != nil {
		return nil, err
	}

	digestAlgorithm, ok := digestAlgorithmsByIdentifier[digestAlgorithmId]
	if !ok {
		return nil, errors.New("Unknown digest algorithm: " + digestAlgorithmId)
	}

	hash := digestAlgorithm.New()
	_, err = hash.Write(data)
	if err != nil {
		return nil, err
	}

	return hash.Sum(nil), nil
}

func (ctx *ValidationContext) verifySignedInfo(sig *types.Signature, canonicalizer Canonicalizer, signatureMethodId string, cert *x509.Certificate, decodedSignature []byte) error {
	signatureElement := sig.UnderlyingElement()

	signedInfo := signatureElement.FindElement(childPath(signatureElement.Space, SignedInfoTag))
	if signedInfo == nil {
		return errors.New("Missing SignedInfo")
	}

	// Canonicalize the xml
	canonical, err := canonicalSerialize(signedInfo)
	if err != nil {
		return err
	}

	signatureAlgorithm, ok := signatureMethodsByIdentifier[signatureMethodId]
	if !ok {
		return errors.New("Unknown signature method: " + signatureMethodId)
	}

	hash := signatureAlgorithm.New()
	_, err = hash.Write(canonical)
	if err != nil {
		return err
	}

	hashed := hash.Sum(nil)

	pubKey, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return errors.New("Invalid public key")
	}

	// Verify that the private key matching the public key from the cert was what was used to sign the 'SignedInfo' and produce the 'SignatureValue'
	err = rsa.VerifyPKCS1v15(pubKey, signatureAlgorithm, hashed[:], decodedSignature)
	if err != nil {
		return err
	}

	return nil
}

func (ctx *ValidationContext) validateSignature(el *etree.Element, sig *types.Signature, cert *x509.Certificate) (*etree.Element, error) {
	idAttr := el.SelectAttr(ctx.IdAttribute)
	if idAttr == nil || idAttr.Value == "" {
		return nil, errors.New("Missing ID attribute")
	}

	var ref *types.Reference

	// Find the first reference which references the top-level element
	for _, _ref := range sig.SignedInfo.References {
		if _ref.URI == "" || _ref.URI[1:] == idAttr.Value {
			ref = &_ref
		}
	}

	// Perform all transformations listed in the 'SignedInfo'
	// Basically, this means removing the 'SignedInfo'
	transformed, canonicalizer, err := ctx.transform(el, sig, ref)
	if err != nil {
		return nil, err
	}

	digestAlgorithm := ref.DigestAlgo.Algorithm

	// Digest the transformed XML and compare it to the 'DigestValue' from the 'SignedInfo'
	digest, err := ctx.digest(transformed, digestAlgorithm, canonicalizer)
	if err != nil {
		return nil, err
	}

	decodedDigestValue, err := base64.StdEncoding.DecodeString(ref.DigestValue)
	if err != nil {
		return nil, err
	}

	if !bytes.Equal(digest, decodedDigestValue) {
		return nil, errors.New("Signature could not be verified")
	}

	// Decode the 'SignatureValue' so we can compare against it
	decodedSignature, err := base64.StdEncoding.DecodeString(sig.SignatureValue.Data)
	if err != nil {
		return nil, errors.New("Could not decode signature")
	}

	// Actually verify the 'SignedInfo' was signed by a trusted source
	signatureMethod := sig.SignedInfo.SignatureMethod.Algorithm
	err = ctx.verifySignedInfo(sig, canonicalizer, signatureMethod, cert, decodedSignature)
	if err != nil {
		return nil, err
	}

	return transformed, nil
}

func contains(roots []*x509.Certificate, cert *x509.Certificate) bool {
	for _, root := range roots {
		if root.Equal(cert) {
			return true
		}
	}
	return false
}

// findSignature searches for a Signature element referencing the passed root element.
func (ctx *ValidationContext) findSignature(el *etree.Element) (*types.Signature, error) {
	idAttr := el.SelectAttr(ctx.IdAttribute)
	if idAttr == nil || idAttr.Value == "" {
		return nil, errors.New("Missing ID attribute")
	}

	var sig *types.Signature

	// Traverse the tree looking for a Signature element
	err := etreeutils.NSFindIterate(el, Namespace, SignatureTag, func(ctx etreeutils.NSContext, el *etree.Element) error {

		found := false
		err := etreeutils.NSFindIterateCtx(ctx, el, Namespace, SignedInfoTag,
			func(ctx etreeutils.NSContext, signedInfo *etree.Element) error {
				// Ignore any SignedInfo that isn't an immediate descendent of Signature.
				if signedInfo.Parent() != el {
					return nil
				}

				detachedSignedInfo, err := etreeutils.NSDetatch(ctx, signedInfo)
				if err != nil {
					return err
				}

				c14NMethod := detachedSignedInfo.FindElement(childPath(detachedSignedInfo.Space, CanonicalizationMethodTag))
				if c14NMethod == nil {
					return errors.New("missing CanonicalizationMethod on Signature")
				}

				c14NAlgorithm := c14NMethod.SelectAttrValue(AlgorithmAttr, "")

				var canonicalSignedInfo *etree.Element

				switch AlgorithmID(c14NAlgorithm) {
				case CanonicalXML10ExclusiveAlgorithmId:
					err := etreeutils.TransformExcC14n(detachedSignedInfo, "")
					if err != nil {
						return err
					}

					// NOTE: TransformExcC14n transforms the element in-place,
					// while canonicalPrep isn't meant to. Once we standardize
					// this behavior we can drop this, as well as the adding and
					// removing of elements below.
					canonicalSignedInfo = detachedSignedInfo

				case CanonicalXML11AlgorithmId:
					canonicalSignedInfo = canonicalPrep(detachedSignedInfo, map[string]struct{}{})

				default:
					return fmt.Errorf("invalid CanonicalizationMethod on Signature: %s", c14NAlgorithm)
				}

				el.RemoveChild(signedInfo)
				el.AddChild(canonicalSignedInfo)

				found = true

				return etreeutils.ErrTraversalHalted
			})
		if err != nil {
			return err
		}

		if !found {
			return errors.New("Missing SignedInfo")
		}

		// Unmarshal the signature into a structured Signature type
		_sig := &types.Signature{}
		err = etreeutils.NSUnmarshalElement(ctx, el, _sig)
		if err != nil {
			return err
		}

		// Traverse references in the signature to determine whether it has at least
		// one reference to the top level element. If so, conclude the search.
		for _, ref := range _sig.SignedInfo.References {
			if ref.URI == "" || ref.URI[1:] == idAttr.Value {
				sig = _sig
				return etreeutils.ErrTraversalHalted
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if sig == nil {
		return nil, ErrMissingSignature
	}

	return sig, nil
}

func (ctx *ValidationContext) verifyCertificate(sig *types.Signature) (*x509.Certificate, error) {
	now := ctx.Clock.Now()

	roots, err := ctx.CertificateStore.Certificates()
	if err != nil {
		return nil, err
	}

	var cert *x509.Certificate

	if sig.KeyInfo != nil {
		// If the Signature includes KeyInfo, extract the certificate from there
		if sig.KeyInfo.X509Data.X509Certificate.Data == "" {
			return nil, errors.New("missing X509Certificate within KeyInfo")
		}

		certData, err := base64.StdEncoding.DecodeString(
			whiteSpace.ReplaceAllString(sig.KeyInfo.X509Data.X509Certificate.Data, ""))
		if err != nil {
			return nil, errors.New("Failed to parse certificate")
		}

		cert, err = x509.ParseCertificate(certData)
		if err != nil {
			return nil, err
		}
	} else {
		// If the Signature doesn't have KeyInfo, Use the root certificate if there is only one
		if len(roots) == 1 {
			cert = roots[0]
		} else {
			return nil, errors.New("Missing x509 Element")
		}
	}

	// Verify that the certificate is one we trust
	if !contains(roots, cert) {
		return nil, errors.New("Could not verify certificate against trusted certs")
	}

	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return nil, errors.New("Cert is not valid at this time")
	}

	return cert, nil
}

// Validate verifies that the passed element contains a valid enveloped signature
// matching a currently-valid certificate in the context's CertificateStore.
func (ctx *ValidationContext) Validate(el *etree.Element) (*etree.Element, error) {
	// Make a copy of the element to avoid mutating the one we were passed.
	el = el.Copy()

	sig, err := ctx.findSignature(el)
	if err != nil {
		return nil, err
	}

	cert, err := ctx.verifyCertificate(sig)
	if err != nil {
		return nil, err
	}

	return ctx.validateSignature(el, sig, cert)
}
