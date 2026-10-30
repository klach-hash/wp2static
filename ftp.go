package main

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// Minimal FTP/FTPS client (explicit TLS) – stdlib only.
type FTP struct {
	host string
	ctrl net.Conn
	r    *textproto.Reader
	tls  *tls.Config // nil = plain FTP
}

func ftpDial(c FTPConfig) (*FTP, error) {
	if c.Host == "" {
		return nil, errors.New(ltr("ftpNoHost"))
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), 20*time.Second)
	if err != nil {
		return nil, err
	}
	f := &FTP{host: c.Host, ctrl: conn}
	f.r = textproto.NewReader(bufio.NewReader(conn))
	if _, _, err := f.expect(2); err != nil {
		conn.Close()
		return nil, err
	}
	if c.TLS {
		if _, err := f.cmd(2, "AUTH TLS"); err != nil {
			conn.Close()
			return nil, fmt.Errorf("%s: %w", ltr("ftpNoTLS"), err)
		}
		f.tls = &tls.Config{ServerName: c.Host, ClientSessionCache: tls.NewLRUClientSessionCache(8)}
		tc := tls.Client(conn, f.tls)
		if err := tc.Handshake(); err != nil {
			conn.Close()
			return nil, err
		}
		f.ctrl = tc
		f.r = textproto.NewReader(bufio.NewReader(tc))
		if _, err := f.cmd(2, "PBSZ 0"); err != nil {
			return nil, err
		}
		if _, err := f.cmd(2, "PROT P"); err != nil {
			return nil, err
		}
	}
	code, _, err := f.send("USER " + c.User)
	if err != nil {
		return nil, err
	}
	if code/100 == 3 {
		if _, err := f.cmd(2, "PASS "+c.Pass); err != nil {
			return nil, fmt.Errorf("%s: %w", ltr("ftpLogin"), err)
		}
	} else if code/100 != 2 {
		return nil, errors.New(Tf(currentLogLang(), "ftpLoginCode", code))
	}
	if _, err := f.cmd(2, "TYPE I"); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *FTP) resp() (int, string, error) {
	line, err := f.r.ReadLine()
	if err != nil {
		return 0, "", err
	}
	if len(line) < 3 {
		return 0, line, errors.New(Tf(currentLogLang(), "ftpBadResp", line))
	}
	code, err := strconv.Atoi(line[:3])
	if err != nil {
		return 0, line, errors.New(Tf(currentLogLang(), "ftpBadResp", line))
	}
	msg := line
	if len(line) > 3 && line[3] == '-' {
		for {
			l, err := f.r.ReadLine()
			if err != nil {
				return code, msg, err
			}
			msg += "\n" + l
			if len(l) >= 4 && l[:3] == line[:3] && l[3] == ' ' {
				break
			}
		}
	}
	return code, msg, nil
}

func (f *FTP) expect(class int) (int, string, error) {
	code, msg, err := f.resp()
	if err != nil {
		return code, msg, err
	}
	if code/100 != class {
		return code, msg, fmt.Errorf("%s", msg)
	}
	return code, msg, nil
}

func (f *FTP) send(line string) (int, string, error) {
	if strings.ContainsAny(line, "\r\n") {
		return 0, "", fmt.Errorf("niedozwolony znak w poleceniu FTP")
	}
	if _, err := fmt.Fprintf(f.ctrl, "%s\r\n", line); err != nil {
		return 0, "", err
	}
	return f.resp()
}

func (f *FTP) cmd(class int, line string) (string, error) {
	code, msg, err := f.send(line)
	if err != nil {
		return msg, err
	}
	if code/100 != class {
		return msg, fmt.Errorf("%s", msg)
	}
	return msg, nil
}

func (f *FTP) dataConn() (net.Conn, error) {
	var host string
	var port int
	if msg, err := f.cmd(2, "EPSV"); err == nil {
		i, j := strings.Index(msg, "(|||"), strings.LastIndex(msg, "|)")
		if i < 0 || j < i {
			return nil, errors.New(Tf(currentLogLang(), "ftpBadEPSV", msg))
		}
		port, _ = strconv.Atoi(msg[i+4 : j])
		host = f.host
	} else {
		msg, err := f.cmd(2, "PASV")
		if err != nil {
			return nil, err
		}
		i, j := strings.Index(msg, "("), strings.Index(msg, ")")
		if i < 0 || j < i {
			return nil, errors.New(Tf(currentLogLang(), "ftpBadPASV", msg))
		}
		p := strings.Split(msg[i+1:j], ",")
		if len(p) != 6 {
			return nil, errors.New(Tf(currentLogLang(), "ftpBadPASV", msg))
		}
		hi, _ := strconv.Atoi(p[4])
		lo, _ := strconv.Atoi(p[5])
		port, host = hi*256+lo, f.host // IP from PASV is sometimes internal – use the server address
	}
	dc, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 20*time.Second)
	if err != nil {
		return nil, err
	}
	return dc, nil
}

func (f *FTP) Store(remote string, r io.Reader) error {
	dc, err := f.dataConn()
	if err != nil {
		return err
	}
	if f.tls != nil {
		dc = tls.Client(dc, f.tls)
	}
	if code, msg, err := f.send("STOR " + remote); err != nil || code/100 != 1 {
		dc.Close()
		if err == nil {
			err = fmt.Errorf("%s", msg)
		}
		return err
	}
	_, cerr := io.Copy(dc, r)
	if c := dc.Close(); cerr == nil {
		cerr = c
	}
	if _, _, err := f.expect(2); err != nil {
		return err
	}
	return cerr
}

func (f *FTP) Mkdir(dir string) { _, _, _ = f.send("MKD " + dir) } // error means it already exists
func (f *FTP) Delete(p string) error {
	_, err := f.cmd(2, "DELE "+p)
	return err
}
func (f *FTP) Close() { _, _, _ = f.send("QUIT"); f.ctrl.Close() }

func (f *FTP) Rmdir(dir string) { _, _, _ = f.send("RMD " + dir) } // a non-empty directory will be rejected
