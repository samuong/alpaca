# syntax=docker/dockerfile:1
#
# End-to-end test fixture for alpaca's multi-method proxy authentication.
#
# Bundles MIT Kerberos (KDC + admin server) and Squid in a single image so
# the Go test only has to manage one container. The image:
#
#   - Runs an MIT KDC for the realm EXAMPLE.TEST, with three principals:
#       * alice@EXAMPLE.TEST          - the test "user" alpaca will obtain
#                                       a TGT for via kinit
#       * HTTP/proxy.example.test@... - squid's service principal, kept in
#                                       /etc/squid/HTTP.keytab
#       * admin/admin@EXAMPLE.TEST    - kadmin master, used by the entrypoint
#
#   - Runs squid configured to advertise Negotiate, NTLM and Basic, with
#       * negotiate_kerberos_auth helper backed by HTTP.keytab
#       * ntlm_fake_auth helper that accepts any well-formed NTLM message
#         from a hard-coded set of credentials (NTLM cannot be tested
#         against a real DC inside this image; the helper validates message
#         shape and fixed credentials only)
#       * basic_ncsa_auth helper backed by /etc/squid/passwd
#
#   - Listens on :3128 and proxies http/https traffic out to the test
#       fixture's loopback (the Go test starts an httptest server inside
#       the container's host network namespace).
#
# All identifiers are deliberately fictitious. Nothing here is bound to
# any real organisation.

# Pinned by digest as defence-in-depth against compromised registry
# mirrors. Refresh with `docker pull debian:bookworm-slim` then update
# the digest below.
FROM debian:bookworm-slim@sha256:67b30a61dc87758f0caf819646104f29ecbda97d920aaf5edc834128ac8493d3

# Build-time proxy support — when building from behind a corporate proxy,
# pass --build-arg HTTP_PROXY=... HTTPS_PROXY=... and apt-get will use it.
# Empty defaults are fine outside that scenario.
ARG HTTP_PROXY=""
ARG HTTPS_PROXY=""
ARG NO_PROXY="localhost,127.0.0.1"
ENV http_proxy=$HTTP_PROXY \
    https_proxy=$HTTPS_PROXY \
    no_proxy=$NO_PROXY

ENV DEBIAN_FRONTEND=noninteractive
ENV REALM=EXAMPLE.TEST
ENV KDC_HOST=kdc.example.test
ENV PROXY_HOST=proxy.example.test

# Test-only credentials. These are NOT secrets — every runner of this
# image has the same passwords, and the image only runs inside the test
# fixture's network namespace. Defining them here keeps the bootstrap
# script generic.
ENV ADMIN_PRINC=admin/admin
ENV USER_PRINC=alice
ENV BASIC_USER=bob

RUN apt-get update \
    && apt-get install --no-install-recommends -y \
         krb5-kdc krb5-admin-server krb5-user \
         squid \
         apache2-utils \
         ca-certificates \
         curl iproute2 \
         procps \
         python3 \
    && rm -rf /var/lib/apt/lists/*

# ---------------------------------------------------------------
# Kerberos configuration
# ---------------------------------------------------------------
RUN cat >/etc/krb5.conf <<EOF
[libdefaults]
    default_realm = ${REALM}
    dns_lookup_kdc = false
    dns_lookup_realm = false
    rdns = false
    forwardable = true
    udp_preference_limit = 1

[realms]
    ${REALM} = {
        kdc = ${KDC_HOST}
        admin_server = ${KDC_HOST}
    }

[domain_realm]
    .example.test = ${REALM}
    example.test = ${REALM}
EOF

RUN cat >/etc/krb5kdc/kdc.conf <<EOF
[kdcdefaults]
    kdc_ports = 88
    kdc_tcp_ports = 88

[realms]
    ${REALM} = {
        database_name = /var/lib/krb5kdc/principal
        admin_keytab = FILE:/etc/krb5kdc/kadm5.keytab
        acl_file = /etc/krb5kdc/kadm5.acl
        key_stash_file = /etc/krb5kdc/stash
        kdc_ports = 88
        max_life = 1d
        max_renewable_life = 7d
        master_key_type = aes256-cts
        supported_enctypes = aes256-cts:normal aes128-cts:normal
        default_principal_flags = +preauth
    }
EOF

RUN echo "${ADMIN_PRINC}@${REALM} *" >/etc/krb5kdc/kadm5.acl

# ---------------------------------------------------------------
# Squid configuration
# ---------------------------------------------------------------
RUN cat >/etc/squid/squid.conf <<'EOF'
# Authentication helpers
auth_param negotiate program /usr/lib/squid/negotiate_kerberos_auth -s HTTP/proxy.example.test@EXAMPLE.TEST
auth_param negotiate children 2 startup=1 idle=1
auth_param negotiate keep_alive on

auth_param ntlm program /usr/lib/squid/ntlm_fake_auth
auth_param ntlm children 2

auth_param basic program /usr/lib/squid/basic_ncsa_auth /etc/squid/passwd
auth_param basic realm proxy.example.test
auth_param basic credentialsttl 30 seconds

# ACL: any authenticated user
acl authenticated proxy_auth REQUIRED
acl Safe_ports port 80
acl Safe_ports port 443
acl Safe_ports port 1025-65535
acl CONNECT method CONNECT
acl SSL_ports port 443

http_access deny !Safe_ports
http_access deny CONNECT !SSL_ports
http_access allow authenticated
http_access deny all

http_port 3128

# Quiet log noise; tests scrape stderr for failures only.
access_log stdio:/var/log/squid/access.log squid
cache_log /var/log/squid/cache.log
pid_filename /run/squid.pid

# No disk cache (keeps the test fast)
cache deny all
EOF

# ---------------------------------------------------------------
# Entrypoint: bootstrap KDC, principals, keytab, basic-auth user file,
# then run squid in the foreground.
# ---------------------------------------------------------------
RUN cat >/usr/local/bin/bootstrap.sh <<'EOF'
#!/bin/bash
set -euo pipefail

REALM="${REALM:-EXAMPLE.TEST}"
PROXY_HOST="${PROXY_HOST:-proxy.example.test}"
USER_PRINC="${USER_PRINC:-alice}"
BASIC_USER="${BASIC_USER:-bob}"
ADMIN_PRINC="${ADMIN_PRINC:-admin/admin}"

# Hard-coded test passwords. Image-only; not secrets.
USER_PASSWORD="alicepw"
BASIC_PASSWORD="bobpw"
ADMIN_PASSWORD="adminpw"

# 1. Create the KDC database (once)
if [[ ! -f /var/lib/krb5kdc/principal ]]; then
    echo "==> Creating KDC database"
    kdb5_util create -r "${REALM}" -s -P masterkey
    kadmin.local -q "addprinc -pw ${ADMIN_PASSWORD} ${ADMIN_PRINC}"
    kadmin.local -q "addprinc -pw ${USER_PASSWORD} ${USER_PRINC}"
    kadmin.local -q "addprinc -randkey HTTP/${PROXY_HOST}"
    kadmin.local -q "ktadd -k /etc/squid/HTTP.keytab HTTP/${PROXY_HOST}"
    chown proxy:proxy /etc/squid/HTTP.keytab
    chmod 640 /etc/squid/HTTP.keytab
fi

# 2. Basic-auth password file
echo "==> Creating Basic-auth user"
htpasswd -bc /etc/squid/passwd "${BASIC_USER}" "${BASIC_PASSWORD}"
chown proxy:proxy /etc/squid/passwd

# 3. Hostname mapping (so kinit + Kerberos see proxy.example.test as 127.0.0.1)
if ! grep -q "${PROXY_HOST}" /etc/hosts; then
    echo "127.0.0.1 ${PROXY_HOST} kdc.example.test" >>/etc/hosts
fi

# 4. Tell squid where the keytab is (negotiate helper reads KRB5_KTNAME)
export KRB5_KTNAME="/etc/squid/HTTP.keytab"
export KRB5_CONFIG="/etc/krb5.conf"

mkdir -p /var/log/squid /run /var/spool/squid
chown -R proxy:proxy /var/log/squid /var/spool/squid

# 5. Initialise squid's swap state (idempotent)
squid -N -z -f /etc/squid/squid.conf 2>/dev/null || true

# 6. Start KDC in the background
echo "==> Starting KDC"
krb5kdc -P /run/krb5kdc.pid

# Wait for the KDC to start listening. If it never does, fail loudly
# rather than silently falling through into the squid startup that
# depends on it — a half-running container is harder to diagnose than
# a clean container start failure.
kdc_ready=0
for i in $(seq 1 20); do
    if (echo > /dev/tcp/127.0.0.1/88) >/dev/null 2>&1; then
        kdc_ready=1
        break
    fi
    sleep 0.2
done
if [ "$kdc_ready" != "1" ]; then
    echo "==> KDC failed to start listening on 127.0.0.1:88 within 4s" >&2
    echo "==> tail /var/log/krb5kdc.log:" >&2
    tail -20 /var/log/krb5kdc.log 2>/dev/null || true
    exit 1
fi

# 7. Start an in-container upstream HTTP server so squid has somewhere
# to forward to. The test asserts a 200 from this server, so its presence
# is what proves alpaca's auth chain reached the "request forwarded"
# stage. Listening on 127.0.0.1:8080 inside the container; squid picks
# this up via the upstream URL the test sends.
mkdir -p /var/upstream && cd /var/upstream
echo 'ok' >index.html
echo "==> Starting upstream HTTP server on :8080"
python3 -m http.server --bind 127.0.0.1 8080 \
    >/var/log/upstream.log 2>&1 &
cd /

# 8. Start squid in the foreground
echo "==> Starting squid"
exec squid -N -f /etc/squid/squid.conf
EOF
RUN chmod +x /usr/local/bin/bootstrap.sh

EXPOSE 3128 88
ENTRYPOINT ["/usr/local/bin/bootstrap.sh"]
