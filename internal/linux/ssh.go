package linux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

const (
	defaultSSHConfigPath = "/etc/ssh/sshd_config"
	sshIncludeBase       = "/etc/ssh"
	maxSSHFileBytes      = 1 << 20
	maxSSHIncludeDepth   = 16
	maxSSHConfigFiles    = 128
)

var (
	errSSHMainRead    = errors.New("configuração principal do SSH ilegível")
	errSSHIncludeRead = errors.New("Include do SSH ilegível")
	errSSHIncludeLoop = errors.New("ciclo de Include do SSH")
	errSSHLimit       = errors.New("limite da configuração SSH atingido")
	errSSHSyntax      = errors.New("sintaxe da configuração SSH incerta")
)

type sshFileReader func(context.Context, string) ([]byte, error)

type sshCheck struct {
	configPath  string
	includeBase string
	readFile    sshFileReader
}

func (sshCheck) Name() string { return "SSH" }

func (c sshCheck) Run(ctx context.Context) check.Result {
	if result, interrupted := contextWarning(ctx); interrupted {
		return result
	}

	path := c.configPath
	if path == "" {
		path = defaultSSHConfigPath
	}
	base := c.includeBase
	if base == "" {
		base = filepath.Dir(path)
	}
	readFile := c.readFile
	if readFile == nil {
		readFile = readSSHFile
	}
	parser := sshConfigParser{
		includeBase: base,
		readFile:    readFile,
		active:      make(map[string]bool),
	}
	err := parser.parseFile(ctx, path, sshScope{}, false, 0)
	if result, interrupted := contextWarning(ctx); interrupted {
		return result
	}
	if err != nil {
		switch {
		case errors.Is(err, errSSHMainRead) && errors.Is(err, os.ErrNotExist):
			return sshWarning("configuração do servidor SSH não encontrada")
		case errors.Is(err, errSSHMainRead):
			return sshWarning("configuração do servidor SSH ilegível")
		case errors.Is(err, errSSHIncludeRead):
			return sshWarning("não foi possível ler o Include do SSH")
		case errors.Is(err, errSSHIncludeLoop):
			return sshWarning("ciclo de Include do SSH detectado")
		case errors.Is(err, errSSHLimit):
			return sshWarning("a configuração SSH excede o limite de inspeção")
		default:
			return sshWarning("não foi possível interpretar a configuração SSH")
		}
	}

	root := parser.root.effective()
	password := parser.password.effective()
	message := rootPolicyMessage(root) + "; " + passwordPolicyMessage(password)
	if root == "no" && password == "no" {
		return check.Result{Status: check.StatusPass, Message: message}
	}
	return sshWarning(message)
}

func sshWarning(message string) check.Result {
	return check.Result{Status: check.StatusWarn, Message: message}
}

func rootPolicyMessage(value string) string {
	switch value {
	case "no":
		return "login de root desativado"
	case "yes":
		return "login de root ativado"
	case "prohibit-password":
		return "login de root limitado a métodos sem senha"
	case "forced-commands-only":
		return "login de root limitado a comandos forçados"
	case "conditional":
		return "login de root condicional"
	default:
		return "login de root desconhecido"
	}
}

func passwordPolicyMessage(value string) string {
	switch value {
	case "no":
		return "autenticação por senha desativada"
	case "yes":
		return "autenticação por senha ativada"
	case "conditional":
		return "autenticação por senha condicional"
	default:
		return "autenticação por senha desconhecida"
	}
}

// readSSHFile accepts only bounded regular files. O_NONBLOCK prevents a
// replaced Include pointing at a FIFO from holding up the scan at open.
func readSSHFile(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errSSHSyntax
	}
	if info.Size() > maxSSHFileBytes {
		return nil, errSSHLimit
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSSHFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSSHFileBytes {
		return nil, errSSHLimit
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

type sshScope struct {
	block int
	all   bool
}

type sshOverride struct {
	value string
	all   bool
}

type sshSignal struct {
	global    string
	overrides []sshOverride
	seen      map[int]bool
}

func (s *sshSignal) set(scope sshScope, value string) {
	if scope.block == 0 {
		if s.global == "" {
			s.global = value // sshd_config uses the first obtained global value.
		}
		return
	}
	if s.seen == nil {
		s.seen = make(map[int]bool)
	}
	if !s.seen[scope.block] {
		s.seen[scope.block] = true
		s.overrides = append(s.overrides, sshOverride{value: value, all: scope.all})
	}
}

func (s sshSignal) effective() string {
	value := s.global
	limit := len(s.overrides)
	for i, override := range s.overrides {
		if override.all {
			value = override.value
			limit = i
			break // An earlier Match all wins over later Match blocks.
		}
	}
	if value == "" {
		if len(s.overrides) > 0 {
			return "conditional"
		}
		return "unknown" // Do not assume OpenSSH's compiled defaults.
	}
	for _, override := range s.overrides[:limit] {
		if override.value != value {
			return "conditional"
		}
	}
	return value
}

type sshConfigParser struct {
	includeBase string
	readFile    sshFileReader
	active      map[string]bool
	files       int
	nextBlock   int
	root        sshSignal
	password    sshSignal
}

func (p *sshConfigParser) parseFile(ctx context.Context, path string, scope sshScope, inheritedConditional bool, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > maxSSHIncludeDepth || p.files >= maxSSHConfigFiles {
		return errSSHLimit
	}
	path = filepath.Clean(path)
	if p.active[path] {
		return errSSHIncludeLoop
	}
	p.active[path] = true
	defer delete(p.active, path)
	p.files++

	data, err := p.readFile(ctx, path)
	if err != nil {
		if errors.Is(err, errSSHLimit) {
			return err
		}
		if depth == 0 {
			return fmt.Errorf("%w: %w", errSSHMainRead, err)
		}
		return fmt.Errorf("%w: %w", errSSHIncludeRead, err)
	}
	if len(data) > maxSSHFileBytes {
		return errSSHLimit
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return errSSHSyntax
	}

	for _, line := range strings.Split(string(data), "\n") {
		if err := ctx.Err(); err != nil {
			return err
		}
		words, err := sshWords(line)
		if err != nil {
			return errSSHSyntax
		}
		if len(words) == 0 {
			continue
		}
		keyword, args := sshDirective(words)
		switch keyword {
		case "match":
			if len(args) == 0 || (strings.EqualFold(args[0], "all") && len(args) != 1) {
				return errSSHSyntax
			}
			p.nextBlock++
			scope = sshScope{block: p.nextBlock, all: len(args) == 1 && strings.EqualFold(args[0], "all") && !inheritedConditional}
		case "include":
			if len(args) == 0 {
				return errSSHSyntax
			}
			for _, pattern := range args {
				childConditional := inheritedConditional || (scope.block != 0 && !scope.all)
				if err := p.parseInclude(ctx, pattern, scope, childConditional, depth+1); err != nil {
					return err
				}
			}
		case "permitrootlogin", "passwordauthentication":
			if len(args) != 1 {
				return errSSHSyntax
			}
			value := strings.ToLower(args[0])
			if keyword == "permitrootlogin" {
				switch value {
				case "no", "yes", "prohibit-password", "forced-commands-only":
				case "without-password":
					value = "prohibit-password" // Documented deprecated alias.
				default:
					return errSSHSyntax
				}
				p.root.set(scope, value)
			} else {
				if value != "no" && value != "yes" {
					return errSSHSyntax
				}
				p.password.set(scope, value)
			}
		}
	}
	return nil
}

func (p *sshConfigParser) parseInclude(ctx context.Context, pattern string, scope sshScope, inheritedConditional bool, depth int) error {
	if pattern == "" || strings.HasPrefix(pattern, "~") {
		return errSSHSyntax // Tilde expansion is outside this focused parser.
	}
	if !filepath.IsAbs(pattern) {
		// OpenSSH resolves relative Include paths against /etc/ssh, not the
		// directory of the file containing the Include.
		pattern = filepath.Join(p.includeBase, pattern)
	}
	if strings.ContainsAny(filepath.Dir(pattern), "*?[") {
		return errSSHSyntax // Avoid silently skipping unreadable wildcard directories.
	}
	if strings.ContainsAny(filepath.Base(pattern), "*?[") {
		if _, err := os.ReadDir(filepath.Dir(pattern)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil // An unmatched glob is valid in sshd_config.
			}
			return fmt.Errorf("%w: %w", errSSHIncludeRead, err)
		}
	}
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return errSSHSyntax
	}
	sort.Strings(paths) // OpenSSH processes Include matches in lexical order.
	patternName := filepath.Base(pattern)
	for _, path := range paths {
		if strings.HasPrefix(filepath.Base(path), ".") && !strings.HasPrefix(patternName, ".") && !strings.HasPrefix(patternName, `\.`) {
			// OpenSSH's glob() does not set GLOB_PERIOD: * and ? do not
			// match a leading dot, unlike filepath.Glob.
			continue
		}
		if err := p.parseFile(ctx, path, scope, inheritedConditional, depth); err != nil {
			return err
		}
	}
	return nil
}

func sshDirective(words []string) (string, []string) {
	keyword, value, hasEquals := strings.Cut(words[0], "=")
	args := words[1:]
	if hasEquals {
		if value != "" {
			args = append([]string{value}, args...)
		}
	} else if len(args) > 0 && strings.HasPrefix(args[0], "=") {
		value = strings.TrimPrefix(args[0], "=")
		args = args[1:]
		if value != "" {
			args = append([]string{value}, args...)
		}
	}
	return strings.ToLower(keyword), args
}

// sshWords follows the small part of OpenSSH's argv_split syntax needed for
// directives and Include paths: whitespace, quotes, basic escapes, and # at
// the start of a token.
func sshWords(line string) ([]string, error) {
	line = strings.TrimRight(line, "\r\f")
	var words []string
	for i := 0; i < len(line); {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		if i == len(line) || line[i] == '#' {
			break
		}
		var word strings.Builder
		var quote byte
		for i < len(line) {
			ch := line[i]
			if ch == '\\' && i+1 < len(line) {
				next := line[i+1]
				if next == '\\' || next == '\'' || next == '"' || (quote == 0 && next == ' ') {
					word.WriteByte(next)
					i += 2
					continue
				}
			}
			if quote == 0 && (ch == ' ' || ch == '\t') {
				break
			}
			if ch == '"' || ch == '\'' {
				if quote == 0 {
					quote = ch
					i++
					continue
				}
				if quote == ch {
					quote = 0
					i++
					continue
				}
			}
			word.WriteByte(ch)
			i++
		}
		if quote != 0 {
			return nil, errSSHSyntax
		}
		words = append(words, word.String())
	}
	return words, nil
}
