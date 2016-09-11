package lib

import (
	"crypto/dsa"
	"crypto/ecdsa"
	"crypto/rsa"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os/user"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/gravitational/teleport/integration"
	"github.com/gravitational/teleport/lib/auth/native"
	"github.com/gravitational/teleport/lib/client"
	"github.com/gravitational/teleport/lib/utils"
	"github.com/gravitational/trace"
)

// Identity defines a user/account of Teleconsole. There are two types of
// identities:
//
// 1. Anonymous identity maps to a local OS user and uses one-time
//    auto-generated SSH pair of keys (priv/pub). By default an anonymous
//    identity is submitted to the proxy server along with a single-use
//    session.
//
//    A joining party receives pub/priv keypair of the anonymous identity
//    and logs in using it.
//
// 2. A named identity uses a user-supplied SSH key, either via github handle
//    or as a file (like ~/.ssh/id_rsa). Named identities private key never
//    leaves the machine, but the joining party is supposed to have a private
//    key on their machine to be able to join.
//
type Identity struct {
	Anonymous bool `json:"anonymous"`
	// username here indicates the local OS user name
	Username string `json:"username"`
	// an identity may have multiple SSH keys, for example Github allows
	// you to have several. also, a user can specify multiple SSH identities
	// for a single session, they all go here:
	Logins []sshLogin `json:"logins"`
}

// sshLogin represents SSH credentials (key, really). The username here
// simply acts as a label
type sshLogin struct {
	Username string      `json:"username"`
	Key      *client.Key `json:"key"`
}

// MakeIdentity creates a new identity from an identity source. If the source
// is an empty string, an anonymous identity is created.
//
// Otherwise a regular (named) identity is created. A source can be a comma-separated
// list of values, where each value can be either a file, or a github handle
//
// Examples:
//		MakeIdentity("filename")
//		MakeIdentity('"/home/my name/.ssh/id_rsa",githubuser')
func MakeIdentity(idPath string) (*Identity, error) {
	var (
		err error
		i   Identity
	)
	osUser, err := user.Current()
	if err != nil {
		return nil, trace.Wrap(err)
	}
	i.Username = osUser.Username
	i.Anonymous = (idPath == "")
	if i.Anonymous {
		i.Logins, err = anonymousLogins()
	} else {
		i.Logins, err = loginsFrom(idPath)
	}
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return &i, nil
}

func MakeIdentityFromFile(idFile string) (*Identity, error) {
	login, err := loginFromFile(idFile)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	osUser, err := user.Current()
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return &Identity{
		Anonymous: false,
		Logins:    []sshLogin{*login},
		Username:  osUser.Username,
	}, nil
}

// anonymousLogins generates one-time Teleport logins for an anonymous user.
func anonymousLogins() ([]sshLogin, error) {
	osUser, err := user.Current()
	if err != nil {
		return nil, trace.Wrap(err)
	}
	// this disables Teleport performance optimization:
	native.PrecalculatedKeysNum = 0
	priv, pub, err := native.New().GenerateKeyPair("")
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return []sshLogin{
		sshLogin{
			Username: osUser.Username,
			Key:      &client.Key{Priv: priv, Pub: pub},
		},
	}, nil
}

// loginsFrom generates SSH logins from the given identity sources
func loginsFrom(idSources string) (logins []sshLogin, err error) {
	r := csv.NewReader(strings.NewReader(idSources))
	fields, err := r.ReadAll()
	if err != nil || len(fields) != 1 {
		return nil, trace.Wrap(err, "Failed parsing identity source: '%s'", idSources)
	}
	for _, idSrc := range fields[0] {
		// identity file (SSH private key)
		if utils.IsFile(idSrc) {
			login, err := loginFromFile(idSrc)
			if err != nil {
				return nil, trace.Wrap(err)
			}
			logins = append(logins, *login)
		} else {
			// github user:
			gl, err := loginsFromGithub(idSrc)
			if err != nil {
				return nil, trace.Wrap(err)
			}
			logins = append(logins, gl...)
		}
	}
	return logins, nil
}

func loginsFromGithub(username string) (logins []sshLogin, err error) {
	keys, err := githubKeysFor(username)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	for i, githubKey := range keys {
		logins = append(logins, sshLogin{
			Username: fmt.Sprintf("%s%d", username, i),
			Key: &client.Key{
				Pub:  []byte(githubKey.Value),
				Priv: nil,
			},
		})
	}
	return logins, nil
}

func loginFromFile(fp string) (*sshLogin, error) {
	bytes, err := ioutil.ReadFile(fp)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	// parse the private key:
	p, err := ssh.ParseRawPrivateKey(bytes)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	// derive the public key from the private one:
	var pubKey ssh.PublicKey = nil
	switch pk := p.(type) {
	case *rsa.PrivateKey:
		pubKey, err = ssh.NewPublicKey(&pk.PublicKey)
	case *dsa.PrivateKey:
		pubKey, err = ssh.NewPublicKey(&pk.PublicKey)
	case *ecdsa.PrivateKey:
		pubKey, err = ssh.NewPublicKey(&pk.PublicKey)
	default:
		return nil, trace.Errorf("Unsupported SSH key format")
	}
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return &sshLogin{
		Username: filepath.Base(fp),
		Key: &client.Key{
			Pub:  ssh.MarshalAuthorizedKey(pubKey),
			Priv: bytes,
		},
	}, nil
}

type GithubKey struct {
	ID    int    `json:"id"`
	Value string `json:"key"`
}

func githubKeysFor(username string) ([]GithubKey, error) {
	var hc http.Client
	resp, err := hc.Get(fmt.Sprintf("https://api.github.com/users/%s/keys", username))
	if err != nil {
		return nil, trace.Wrap(err)
	}
	defer resp.Body.Close()
	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	var keys []GithubKey
	if err = json.Unmarshal(bytes, &keys); err != nil {
		return nil, trace.Wrap(err)
	}
	return keys, nil
}

type UserMap map[string]*integration.User

// LoginUsers returns Teleport users suitable for logging into a locally
// running Teleport instance. Public and private keys are returned.
func (this *Identity) LoginUsers() UserMap {
	m := make(UserMap)
	for _, login := range this.Logins {
		logins := []string{this.Username}
		if this.Username != login.Username {
			logins = append(logins, login.Username)
		}
		m[login.Username] = &integration.User{
			Username: login.Username,
			Key: &client.Key{
				Pub:  login.Key.Pub,
				Priv: login.Key.Priv,
			},
			AllowedLogins: logins,
		}
	}
	return m
}

// AnnounceUsers returns a list of Teleport users to be sent along with
// a new Teleconsole session. Anonymous identities send private keys too,
// while regular identities do not send their private keys.
func (this *Identity) AnnounceUsers() UserMap {
	users := this.LoginUsers()
	if !this.Anonymous {
		for _, u := range users {
			u.Key.Priv = nil
		}
	}
	return users
}

func (this *Identity) ToJSON() string {
	b, err := json.MarshalIndent(this, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(b)
}

func (this *Identity) PrivateKeyFor(publicKey []byte) []byte {
	pk := strings.TrimSpace(string(publicKey))
	for _, l := range this.Logins {
		if strings.TrimSpace(string(l.Key.Pub)) == pk {
			return l.Key.Priv
		}
	}
	return nil
}
