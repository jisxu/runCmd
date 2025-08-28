package main

import (
	"bufio"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

//go:embed config.txt
var embeddedConfig embed.FS

const externalConfigFile = "config.txt"

// 命令组结构
type Config struct {
	Settings map[string]string
	Groups   map[string][]string
}

// 解析配置内容（从字符串）
func parseConfig(content string) *Config {
	cfg := &Config{
		Settings: make(map[string]string),
		Groups:   make(map[string][]string),
	}

	var currentGroup string
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 检测分组
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentGroup = strings.Trim(line, "[]")
			if currentGroup != "settings" {
				cfg.Groups[currentGroup] = []string{}
			}
			continue
		}

		// settings 配置
		if currentGroup == "settings" {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				cfg.Settings[key] = val
			}
		} else if currentGroup != "" {
			cfg.Groups[currentGroup] = append(cfg.Groups[currentGroup], line)
		}
	}

	return cfg
}

// 合并配置（外部覆盖默认）
func mergeConfig(base, override *Config) *Config {
	result := &Config{
		Settings: make(map[string]string),
		Groups:   make(map[string][]string),
	}

	// base
	for k, v := range base.Settings {
		result.Settings[k] = v
	}
	for g, cmds := range base.Groups {
		result.Groups[g] = append([]string{}, cmds...)
	}

	// override 覆盖
	for k, v := range override.Settings {
		result.Settings[k] = v
	}
	for g, cmds := range override.Groups {
		result.Groups[g] = append([]string{}, cmds...)
	}

	return result
}

// 在目录执行命令组
func runCmdsInDir(dir string, cmds []string, wg *sync.WaitGroup, worker chan struct{}) {
	defer wg.Done()
	worker <- struct{}{}
	defer func() { <-worker }()

	fmt.Printf(">>> 开始在目录 [%s] 执行命令...\n", dir)

	script := strings.Join(cmds, "\n")
	c := exec.Command("sh", "-c", script)
	c.Dir = dir

	// 合并 stdout 和 stderr
	pipe, _ := c.StdoutPipe()
	c.Stderr = c.Stdout

	if err := c.Start(); err != nil {
		fmt.Printf("[%s] 启动失败: %v\n", dir, err)
		return
	}

	// 实时读取合并后的输出
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		fmt.Printf("[%s] %s\n", dir, scanner.Text())
	}

	if err := c.Wait(); err != nil {
		fmt.Printf("[%s] 执行错误: %v\n", dir, err)
	}
	fmt.Printf("<<< 完成目录 [%s] 的命令执行\n\n", dir)
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("用法: ./runCmd <group> <dir1> <dir2> ...")
		return
	}

	group := os.Args[1]
	dirs := os.Args[2:]

	// 先加载内嵌配置
	data, _ := embeddedConfig.ReadFile("config.txt")
	cfg := parseConfig(string(data))

	// 如果存在外部 config.txt，覆盖
	if ext, err := os.ReadFile(externalConfigFile); err == nil {
		fmt.Printf("检测到外部配置 %s，将覆盖默认配置\n", externalConfigFile)
		override := parseConfig(string(ext))
		cfg = mergeConfig(cfg, override)
	}

	cmds, ok := cfg.Groups[group]
	if !ok {
		fmt.Printf("未找到组 [%s] 的命令，请检查配置\n", group)
		return
	}

	// 并发控制，默认 3
	concurrency := 3
	if v, ok := cfg.Settings["concurrency"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			concurrency = n
		}
	}
	fmt.Printf("最大并发数: %d\n", concurrency)

	worker := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, dir := range dirs {
		wg.Add(1)
		go runCmdsInDir(dir, cmds, &wg, worker)
	}
	wg.Wait()
}
