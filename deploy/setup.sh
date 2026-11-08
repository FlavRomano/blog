#!/bin/sh
# One-time provisioning of the Raspberry Pi. Run as root from the repo's deploy/ dir:
#   sudo ./setup.sh "<ssh public key for the CI deploy user>"
set -eu
[ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }
[ $# -eq 1 ] || { echo "usage: setup.sh '<deploy public key>'" >&2; exit 1; }
HERE=$(cd "$(dirname "$0")" && pwd)

apt-get install -y --no-install-recommends curl file >/dev/null

id blog >/dev/null 2>&1 || useradd --system --home-dir /var/lib/blog --shell /usr/sbin/nologin blog
id deploy >/dev/null 2>&1 || useradd --create-home --shell /bin/bash deploy
passwd -l deploy >/dev/null

install -d -o root -g root -m 0755 /opt/blog
install -d -o blog -g blog -m 0750 /var/lib/blog /var/lib/blog/uploads
install -d -o root -g blog -m 0750 /etc/blog

install -d -o deploy -g deploy -m 0700 /home/deploy/.ssh
install -d -o deploy -g deploy -m 0700 /home/deploy/incoming
printf '%s\n' "$1" > /home/deploy/.ssh/authorized_keys
chown deploy:deploy /home/deploy/.ssh/authorized_keys
chmod 600 /home/deploy/.ssh/authorized_keys

install -o root -g root -m 0755 "$HERE/blog-deploy" /usr/local/bin/blog-deploy
echo 'deploy ALL=(root) NOPASSWD: /usr/local/bin/blog-deploy' > /etc/sudoers.d/blog-deploy
chmod 440 /etc/sudoers.d/blog-deploy
visudo -cf /etc/sudoers.d/blog-deploy >/dev/null

install -o root -g root -m 0644 "$HERE/blog.service" /etc/systemd/system/blog.service
systemctl daemon-reload
systemctl enable blog >/dev/null

if [ ! -f /etc/blog/blog.env ]; then
  install -o root -g blog -m 0640 "$HERE/blog.env.example" /etc/blog/blog.env
  sed -i "s|^BLOG_TOTP_KEY=.*|BLOG_TOTP_KEY=$(openssl rand -hex 32)|; s|^BLOG_IP_SALT=.*|BLOG_IP_SALT=$(openssl rand -hex 32)|" /etc/blog/blog.env
  echo "created /etc/blog/blog.env with fresh secrets: set BLOG_BASE_URL and back the file up"
fi
echo "done. The first CI deploy will start the service."
