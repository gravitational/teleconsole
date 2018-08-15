package lib

import (
	"os/user"
	"testing"
)

func TestAnonymous(t *testing.T) {
	me, _ := user.Current()
	i, err := MakeIdentity("")
	if err != nil {
		t.Fatal(err)
	}
	if !i.Anonymous {
		t.Fatal("supposed to be anonymous")
	}
	if i.Username != me.Username {
		t.Fatalf("anonymous username is '%s', not '%s'", i.Username, me.Username)
	}
	validateUserMap := func(um UserMap) {
		if len(um) != 1 {
			t.Fatalf("usermap len for anonymous identity should be 1, not %d", len(um))
		}
		// examine the teleport login:
		tl := um[me.Username]
		logins := tl.AllowedLogins
		if len(logins) != 1 {
			t.Fatalf("anonymous logins are invalid: %v", logins)
		}
		if logins[0] != me.Username {
			t.Fatalf("anonymous logins do not contain me")
		}
		if tl.Username != me.Username {
			t.Fatalf("teleport login has incorrect username")
		}
		if len(tl.Key.Priv) == 0 || len(tl.Key.Pub) == 0 {
			t.Fatalf("One of the keys for anonymous login is empty")
		}
	}
	validateUserMap(i.LoginUsers())
	validateUserMap(i.AnnounceUsers())
}

func TestNamedIdentity(t *testing.T) {
	me, _ := user.Current()
	// this identity includes 4 keys:
	//   first two are from SSH files in 'fixtures/ids' dir
	//   last two are from Github's "kontsevoy" user
	i, err := MakeIdentity("../fixtures/ids/one,../fixtures/ids/two,kontsevoy")
	if err != nil {
		t.Fatal(err)
	}
	js := i.ToJSON()
	if i.Username != me.Username {
		t.Fatalf("anonymous username is '%s', not '%s'", i.Username, me.Username)
	}
	if i.Anonymous {
		t.Fatal("supposed to NOT be anonymous")
	}
	if len(i.Logins) != 3 {
		t.Fatalf("This identity must have 3 logins, but instead:\n%s", js)
	}
	one := i.Logins[0]
	two := i.Logins[1]
	k1 := i.Logins[2]
	if one.Username == two.Username || one.Username == k1.Username {
		t.Fatal("usernames must not be the same")
	}
	validateFileBased := func(username string, l sshLogin) {
		if username != l.Username {
			t.Fatalf("%s != %s", username, l.Username)
		}
		if len(l.Key.Pub) == 0 || len(l.Key.Priv) == 0 {
			t.Fatalf("one of the keys is empty")
		}
	}
	validateGithubBased := func(username string, l sshLogin) {
		if username != l.Username {
			t.Fatalf("%s != %s", username, l.Username)
		}
		if len(l.Key.Pub) == 0 {
			t.Fatal("Github logins must have a public key")
		}
		if len(l.Key.Priv) != 0 {
			t.Fatal("Github logins must NOT have a private key")
		}
	}
	validateFileBased("one", one)
	validateFileBased("two", two)
	validateGithubBased("kontsevoy0", k1)

	validateUserMap := func(logins UserMap) {
		names := []string{"one", "two", "kontsevoy0"}
		for _, n := range names {
			l := logins[n]
			if l == nil {
				t.Fatalf("login %s is not found", n)
			}
			if l.AllowedLogins[0] != me.Username {
				t.Fatalf("first allowed login must always be local: %s. It is not: %v",
					me.Username, l.AllowedLogins)
			}
			if l.AllowedLogins[1] != n {
				t.Fatalf("second allowed login must be: %s. It is not: %v",
					n, l.AllowedLogins)
			}
			if len(l.Key.Pub) == 0 {
				t.Fatalf("public keys are always on")
			}
		}
	}
	l := i.LoginUsers()
	validateUserMap(l)

	l = i.AnnounceUsers()
	validateUserMap(l)
	for _, u := range l {
		if len(u.Key.Priv) != 0 {
			t.Fatalf("Private keys must never be exposed")
		}
	}
}
