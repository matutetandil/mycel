# An SSH server that will run a command.
#
# The stack's SFTP server will not: it is restricted to the sftp subsystem,
# which is the point of it. `exec { driver = "ssh" }` opens a session and runs
# something, so it needs a server that allows one.
FROM alpine:3.24

RUN apk add --no-cache openssh-server && \
    adduser -D -s /bin/sh runner && \
    passwd -u runner && \
    mkdir -p /home/runner/.ssh && \
    chown -R runner:runner /home/runner/.ssh && \
    chmod 700 /home/runner/.ssh && \
    ssh-keygen -A

# Keys only: a password cannot be typed into a connector.
RUN printf '%s\n' \
    'PermitRootLogin no' \
    'PasswordAuthentication no' \
    'PubkeyAuthentication yes' \
    'AuthorizedKeysFile /home/runner/.ssh/authorized_keys' \
    'AllowUsers runner' \
    > /etc/ssh/sshd_config.d/mycel.conf

EXPOSE 22
CMD ["/usr/sbin/sshd", "-D", "-e"]
