package backup

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// connectSSH establishes an SSH connection using password or key auth
func connectSSH(host string, port int, user, password, keyPath string) (*ssh.Client, error) {
	var authMethods []ssh.AuthMethod

	// If keyPath is provided, try key-based auth first
	if keyPath != "" {
		key, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("读取 SSH 密钥失败 %s: %w", keyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("解析 SSH 密钥失败: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	// Also try password if provided (as fallback or primary)
	if password != "" {
		authMethods = append(authMethods, ssh.Password(password))
	}

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("需要提供密码或 SSH 密钥路径")
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("连接 %s 失败: %w", addr, err)
	}

	c, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SSH 认证失败: %w", err)
	}

	return ssh.NewClient(c, chans, reqs), nil
}

// sftpWrapper wraps the pkg/sftp Client to implement extra methods
type sftpWrapper struct {
	*sftp.Client
}

func createSFTPClient(sshClient *ssh.Client) (*sftpWrapper, error) {
	client, err := sftp.NewClient(sshClient)
	if err != nil {
		return nil, err
	}
	return &sftpWrapper{Client: client}, nil
}

// MkdirAll creates a directory and all parent directories on the remote host
func (s *sftpWrapper) MkdirAll(path string) error {
	if path == "" || path == "/" || path == "." {
		return nil
	}

	// Try to stat the path first
	_, err := s.Stat(path)
	if err == nil {
		return nil // already exists
	}

	// Create parent first
	parent := dir(path)
	if parent != "" && parent != "/" && parent != "." {
		if err := s.MkdirAll(parent); err != nil {
			return err
		}
	}

	return s.Mkdir(path)
}

func dir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			if i == 0 {
				return "/"
			}
			return p[:i]
		}
	}
	return ""
}
