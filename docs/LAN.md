# The LAN try-out stack

`make lan LAN_HOST=jr-pc1.jrmagta.home` runs oh-my-story for a home network, separate from the dev stack. Everything is built from the working tree:

| Service | People reach it at | On this machine |
|---|---|---|
| oh-my-story, over HTTPS (Caddy in front of the app) | `https://jr-pc1.jrmagta.home` | `127.0.0.1:8443` (and `:8090`, which redirects to HTTPS) |
| Keycloak, realm `oms` | `https://jr-pc1.jrmagta.home:8444` (admin console at `/admin`) | `127.0.0.1:8444` |
| Mailpit, where invite emails land | not exposed | `http://127.0.0.1:8026` |

Its database and Keycloak data live in podman volumes and survive restarts. `make lan-down` stops it.

## Certificates

The first `make lan` runs `deploy/lan/pki/make-certs.sh`, which writes a test PKI to `deploy/lan/pki/out/`, outside git:

- `ca.crt`: the test CA, **OMS LAN Test CA**. Devices that trust it see the sites as secure.
- `server.crt` / `server.key`: HTTPS for both sites, for `LAN_HOST`, `localhost` and the container names.
- `users/<name>.p12`: a client certificate per person, for signing in to Keycloak without a password. The person's email is in the certificate, which is how Keycloak finds their account.
- `users.txt`: the people, their passwords, and each `.p12` file's password. `secrets.env`: the same, plus the Keycloak admin password and the app's client secret.

Running it again keeps what exists, so certificates you have imported stay valid. Delete `out/` to start over; then reset Keycloak too (below).

## People in the LAN Keycloak

| Username | Name | Email |
|---|---|---|
| `jonathan` | Jonathan Magtalas | `jonathan@jrmagta.home` |
| `alice` | Alice Example | `alice@example.com` |
| `bob` | Bob Example | `bob@example.com` |
| `carol` | Carol Directory | `carol@example.com` |

Each can sign in with their certificate or with their password from `users.txt`. More people can be added in the Keycloak admin console; a certificate for them needs a line in `make-certs.sh`.

## Setting up Windows (once)

**1. Forward the ports** (PowerShell as administrator). Add these the same way as the existing port 80 rule, which now redirects to HTTPS:

```powershell
netsh interface portproxy add v4tov4 listenaddress=0.0.0.0 listenport=443  connectaddress=127.0.0.1 connectport=8443
netsh interface portproxy add v4tov4 listenaddress=0.0.0.0 listenport=8444 connectaddress=127.0.0.1 connectport=8444
New-NetFirewallRule -DisplayName "oh-my-story LAN HTTPS" -Direction Inbound -Protocol TCP -LocalPort 443,8444 -Action Allow
```

**2. Copy the CA and your certificate to Windows** (in WSL):

```sh
WINUSER=<your Windows user name>
mkdir -p /mnt/c/Users/$WINUSER/Downloads/oms-lan
cp deploy/lan/pki/out/ca.crt deploy/lan/pki/out/users/jonathan.p12 /mnt/c/Users/$WINUSER/Downloads/oms-lan/
```

**3. Trust the test CA** (PowerShell as administrator), so Edge and Chrome accept both sites:

```powershell
Import-Certificate -FilePath "$HOME\Downloads\oms-lan\ca.crt" -CertStoreLocation Cert:\LocalMachine\Root
```

Firefox keeps its own list: Settings → Privacy & Security → Certificates → View Certificates → Authorities → Import.

**4. Install your certificate** (PowerShell, as yourself). The password is jonathan's "file password" in `users.txt`:

```powershell
Import-PfxCertificate -FilePath "$HOME\Downloads\oms-lan\jonathan.p12" -CertStoreLocation Cert:\CurrentUser\My -Password (Read-Host -AsSecureString "p12 password")
```

Or double-click the `.p12` file. Other devices need the CA and, to sign in with a certificate, a `.p12` of their own.

## Signing in with your certificate

1. Open `https://jr-pc1.jrmagta.home` and choose **Sign in with Keycloak**.
2. The browser asks which certificate to use. Choose **Jonathan Magtalas (OMS LAN)**.
3. Keycloak shows the certificate and "You will be logged in as: jonathan". Choose **Sign in**.

Without a certificate, Keycloak shows its username and password form instead.

Signing up stays invite-only, so the first time, give that account access. Sign in with your existing password account (it owns `cheshire-cat`), open **Members → Add from Keycloak**, find "Jonathan Magtalas" and add him, for example as an admin. The Keycloak account (`jonathan@jrmagta.home`) is a separate account from your password one.

## Starting over with Keycloak

The realm is imported once, the first time Keycloak starts. After changing `LAN_HOST`, `LAN_HTTPS_PORT` or `deploy/lan/keycloak/oms-realm.json`, run `make lan-reset-keycloak`, then `make lan`. That forgets anything changed in Keycloak's admin console; oh-my-story's own data is kept.
