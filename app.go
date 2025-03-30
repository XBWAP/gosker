package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/things-go/go-socks5"
)

// SocksRule represents a SOCKS5 server rule
type SocksRule struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	NoAuth        bool   `json:"noAuth"`
	Running       bool   `json:"running"`
	EnableUDP     bool   `json:"enableUDP"`
	UploadBytes   int64  `json:"uploadBytes"`
	DownloadBytes int64  `json:"downloadBytes"`
}

// App struct
type App struct {
	ctx       context.Context
	rules     []SocksRule
	servers   map[string]*socks5.Server
	listeners map[string]net.Listener
	mu        sync.Mutex
	counters  map[string]*TrafficCounter // 流量计数器映射
}

// TrafficCounter 用于统计流量的结构体
type TrafficCounter struct {
	mu            sync.Mutex
	uploadBytes   int64
	downloadBytes int64
	app           *App      // 对App的引用
	ruleID        string    // 规则ID
	lastSync      time.Time // 上次同步时间
}

// NewTrafficCounter 创建新的流量计数器
func NewTrafficCounter(app *App, ruleID string) *TrafficCounter {
	return &TrafficCounter{
		app:      app,
		ruleID:   ruleID,
		lastSync: time.Now(),
	}
}

// CountUpload 统计上传流量
func (t *TrafficCounter) CountUpload(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.uploadBytes += int64(n)

	// 每隔一段时间将流量数据同步到规则中
	if time.Since(t.lastSync) > 3*time.Second {
		t.syncToRule()
	}
}

// CountDownload 统计下载流量
func (t *TrafficCounter) CountDownload(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.downloadBytes += int64(n)

	// 每隔一段时间将流量数据同步到规则中
	if time.Since(t.lastSync) > 3*time.Second {
		t.syncToRule()
	}
}

// 将流量数据同步到规则中
func (t *TrafficCounter) syncToRule() {
	// 已经持有t.mu锁，不要在这里再次获取

	// 获取App锁以更新规则
	t.app.mu.Lock()
	defer t.app.mu.Unlock()

	// 更新对应规则的流量数据
	for i, rule := range t.app.rules {
		if rule.ID == t.ruleID {
			t.app.rules[i].UploadBytes += t.uploadBytes
			t.app.rules[i].DownloadBytes += t.downloadBytes
			// 重置计数器
			t.uploadBytes = 0
			t.downloadBytes = 0
			t.lastSync = time.Now()

			// 每10次流量更新，保存一次规则数据
			if i%10 == 0 {
				go t.app.saveRules() // 使用协程异步保存，避免阻塞
			}

			break
		}
	}
}

// GetStats 获取流量统计
func (t *TrafficCounter) GetStats() (int64, int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.uploadBytes, t.downloadBytes
}

// Reset 重置流量统计
func (t *TrafficCounter) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.uploadBytes = 0
	t.downloadBytes = 0
}

// 自定义连接包装器，用于统计流量
type TrafficConnWrapper struct {
	net.Conn
	counter *TrafficCounter
}

// Read 重写Read方法统计下载流量
func (c *TrafficConnWrapper) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.counter.CountDownload(n)
	}
	return n, err
}

// Write 重写Write方法统计上传流量
func (c *TrafficConnWrapper) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if n > 0 {
		c.counter.CountUpload(n)
	}
	return n, err
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		rules:     []SocksRule{},
		servers:   make(map[string]*socks5.Server),
		listeners: make(map[string]net.Listener),
		counters:  make(map[string]*TrafficCounter),
	}
}

// 规则配置文件路径
const configFile = "gosker_rules.json"

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// 启动时加载保存的规则
	a.loadRules()

	// 打印加载信息，帮助调试
	fmt.Printf("应用启动，成功加载 %d 条规则\n", len(a.rules))
}

// shutdown is called when the app is about to shutdown
func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 保存规则数据
	err := a.saveRules()
	if err != nil {
		fmt.Printf("保存规则失败: %v\n", err)
	} else {
		fmt.Printf("成功保存 %d 条规则到配置文件\n", len(a.rules))
	}

	// 停止所有运行中的服务器
	for id := range a.servers {
		a.stopServerLocked(id)
	}

	// 清空资源
	a.servers = make(map[string]*socks5.Server)
	a.listeners = make(map[string]net.Listener)
	a.counters = make(map[string]*TrafficCounter)
}

// GetRules returns all SOCKS5 rules
func (a *App) GetRules() []SocksRule {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rules
}

// AddRule adds a new SOCKS5 rule
func (a *App) AddRule(rule SocksRule) string {
	a.mu.Lock()
	defer a.mu.Unlock()

	if rule.ID == "" {
		rule.ID = fmt.Sprintf("rule_%d", len(a.rules)+1)
	}

	a.rules = append(a.rules, rule)

	// 保存规则
	a.saveRules()

	return rule.ID
}

// UpdateRule updates an existing SOCKS5 rule
func (a *App) UpdateRule(rule SocksRule) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	for i, r := range a.rules {
		if r.ID == rule.ID {
			// 保存原来的流量统计数据和运行状态
			originalUploadBytes := a.rules[i].UploadBytes
			originalDownloadBytes := a.rules[i].DownloadBytes
			wasRunning := r.Running

			// 停止服务器之前先记录当前状态
			if wasRunning {
				// 停止服务器
				a.stopServerLocked(rule.ID)
			}

			// 更新规则，但保留原始流量数据
			rule.UploadBytes = originalUploadBytes
			rule.DownloadBytes = originalDownloadBytes
			a.rules[i] = rule

			// 如果原来是运行状态，则重新启动
			if wasRunning && rule.Running {
				// 重新启动服务器
				a.startServerLocked(rule.ID)
			}

			// 保存规则
			a.saveRules()

			return true
		}
	}

	return false
}

// DeleteRule deletes a SOCKS5 rule
func (a *App) DeleteRule(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	for i, rule := range a.rules {
		if rule.ID == id {
			if rule.Running {
				a.stopServerLocked(id)
			}

			// Remove the rule
			a.rules = append(a.rules[:i], a.rules[i+1:]...)

			// 保存规则
			a.saveRules()

			return true
		}
	}

	return false
}

// StartServer starts a SOCKS5 server for the given rule ID
func (a *App) StartServer(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.startServerLocked(id)
}

// startServerLocked starts a SOCKS5 server (must be called with lock held)
func (a *App) startServerLocked(id string) bool {
	var rule *SocksRule

	// Find the rule
	for i := range a.rules {
		if a.rules[i].ID == id {
			rule = &a.rules[i]
			break
		}
	}

	if rule == nil {
		return false
	}

	// Check if server is already running
	if rule.Running {
		return true
	}

	// 创建流量计数器
	a.counters[id] = NewTrafficCounter(a, id)

	// 创建socks5服务器选项
	var serverOpts []socks5.Option

	// 如果启用UDP，设置BindIP
	if rule.EnableUDP {
		// 使用0.0.0.0作为绑定IP，允许所有网络接口的UDP连接
		serverOpts = append(serverOpts, socks5.WithBindIP(net.ParseIP("0.0.0.0")))

		// 配置UDP专用选项 - 启用ASSOCIATE命令支持UDP
		permitCommand := &socks5.PermitCommand{
			EnableConnect:   true,
			EnableBind:      true,
			EnableAssociate: true, // 确保开启UDP关联功能
		}
		serverOpts = append(serverOpts, socks5.WithRule(permitCommand))

		// 可选：添加调试日志
		fmt.Printf("启用UDP转发支持，端口: %d\n", rule.Port)
	}

	// 设置身份验证方法
	if !rule.NoAuth {
		// 使用用户名/密码认证
		creds := socks5.StaticCredentials{
			rule.Username: rule.Password,
		}
		serverOpts = append(serverOpts, socks5.WithCredential(creds))
	}

	// 创建SOCKS5服务器
	server := socks5.NewServer(serverOpts...)

	// 启动监听
	addr := fmt.Sprintf(":%d", rule.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}

	// 创建一个带流量统计的监听器包装
	statsListener := &StatsListener{
		Listener: listener,
		counter:  a.counters[id],
		app:      a,
		ruleID:   id,
	}

	// 对于UDP转发，显式测试UDP端口可用性
	if rule.EnableUDP {
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			fmt.Printf("UDP地址解析错误: %v\n", err)
		} else {
			// 尝试创建UDP连接，验证端口是否可用
			udpConn, err := net.ListenUDP("udp", udpAddr)
			if err != nil {
				fmt.Printf("UDP端口 %d 监听失败: %v\n", rule.Port, err)
			} else {
				fmt.Printf("UDP端口 %d 监听成功，服务已准备好接收UDP流量\n", rule.Port)
				// 关闭测试连接，让SOCKS5服务器自己管理UDP监听
				udpConn.Close()
			}
		}
	}

	// 存储服务器和监听器
	a.servers[id] = server
	a.listeners[id] = statsListener

	// 在goroutine中启动服务
	go func() {
		server.Serve(statsListener)
	}()

	// 更新规则状态
	rule.Running = true

	// 保存规则状态
	a.saveRules()

	return true
}

// StatsListener 是一个带有流量统计功能的监听器包装器
type StatsListener struct {
	net.Listener
	counter *TrafficCounter
	app     *App
	ruleID  string
}

// Accept 接受一个连接并包装它以进行流量统计
func (l *StatsListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	// 包装连接以进行流量统计
	return &TrafficConnWrapper{
		Conn:    conn,
		counter: l.counter,
	}, nil
}

// Close 关闭监听器并执行清理
func (l *StatsListener) Close() error {
	// 在关闭监听器之前确保流量数据已安全同步
	if l.counter != nil {
		// 使用recover来防止任何可能的panic
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("监听器关闭过程中发生panic: %v\n", r)
			}
		}()

		// 尝试同步流量数据，但使用超时保护避免长时间阻塞
		syncDone := make(chan bool, 1)
		go func() {
			l.counter.mu.Lock()
			defer l.counter.mu.Unlock()

			if l.counter.uploadBytes > 0 || l.counter.downloadBytes > 0 {
				// 尝试同步，但不再调用syncToRule (可能会导致死锁)
				// 而是直接在stopServerLocked中处理流量统计
			}
			syncDone <- true
		}()

		// 等待同步完成或超时
		select {
		case <-syncDone:
			// 同步成功
		case <-time.After(1 * time.Second):
			// 同步超时，继续关闭过程
			fmt.Println("流量同步超时，继续关闭过程")
		}
	}

	// 最后关闭底层监听器
	return l.Listener.Close()
}

// StopServer stops a SOCKS5 server for the given rule ID
func (a *App) StopServer(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.stopServerLocked(id)
}

// stopServerLocked stops a SOCKS5 server (must be called with lock held)
func (a *App) stopServerLocked(id string) bool {
	// Find the rule
	var rule *SocksRule
	for i := range a.rules {
		if a.rules[i].ID == id {
			rule = &a.rules[i]
			break
		}
	}

	if rule == nil || !rule.Running {
		return false
	}

	// 防止并发崩溃问题，先标记为停止状态
	rule.Running = false

	// 先保存当前的流量统计数据，以防在关闭过程中发生崩溃
	if counter, ok := a.counters[id]; ok && counter != nil {
		counter.mu.Lock()
		if counter.uploadBytes > 0 || counter.downloadBytes > 0 {
			// 手动同步流量数据
			for i, r := range a.rules {
				if r.ID == id {
					a.rules[i].UploadBytes += counter.uploadBytes
					a.rules[i].DownloadBytes += counter.downloadBytes
					break
				}
			}
		}
		counter.mu.Unlock()
	}

	// 删除计数器和服务器
	delete(a.counters, id)
	delete(a.servers, id)

	// Close the listener - 这通常会触发StatsListener.Close()方法
	// 但现在我们已经保存了流量数据，所以这里即使出错也没关系
	if listener, ok := a.listeners[id]; ok {
		err := listener.Close()
		if err != nil {
			fmt.Printf("关闭监听器错误: %v\n", err)
		}
		delete(a.listeners, id)
	}

	// 保存当前的规则状态
	a.saveRules()

	return true
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

// TestUDP 测试指定规则的UDP转发功能
func (a *App) TestUDP(id string) string {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 找到规则
	var rule *SocksRule
	for i := range a.rules {
		if a.rules[i].ID == id {
			rule = &a.rules[i]
			break
		}
	}

	if rule == nil {
		return "错误：未找到规则"
	}

	if !rule.Running {
		return "错误：服务器未运行，请先启动服务器"
	}

	if !rule.EnableUDP {
		return "错误：未启用UDP转发，请先启用UDP功能"
	}

	// 测试UDP端口是否可以监听
	addr := fmt.Sprintf(":%d", rule.Port)
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Sprintf("UDP地址解析错误: %v", err)
	}

	// 尝试创建UDP连接，验证端口是否可用
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Sprintf("UDP端口 %d 监听失败: %v", rule.Port, err)
	}

	// 关闭测试连接
	udpConn.Close()

	return fmt.Sprintf("UDP端口 %d 测试成功：端口可用于UDP通信", rule.Port)
}

// GetTrafficStats 返回指定规则的流量统计
func (a *App) GetTrafficStats(id string) (int64, int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, rule := range a.rules {
		if rule.ID == id {
			return rule.UploadBytes, rule.DownloadBytes, nil
		}
	}

	return 0, 0, fmt.Errorf("未找到规则: %s", id)
}

// ResetTrafficStats 重置指定规则的流量统计
func (a *App) ResetTrafficStats(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	for i, rule := range a.rules {
		if rule.ID == id {
			a.rules[i].UploadBytes = 0
			a.rules[i].DownloadBytes = 0

			// 保存规则
			a.saveRules()

			return nil
		}
	}

	return fmt.Errorf("未找到规则: %s", id)
}

// 保存规则到文件
func (a *App) saveRules() error {
	// 创建一个不包含运行状态的规则副本
	saveRules := make([]SocksRule, len(a.rules))
	for i, rule := range a.rules {
		saveRule := rule
		saveRule.Running = false // 保存时将所有规则标记为未运行
		saveRules[i] = saveRule
	}

	// 序列化规则
	data, err := json.MarshalIndent(saveRules, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化规则失败: %v", err)
	}

	// 获取配置文件路径
	configPath := getConfigFilePath()

	// 确保配置目录存在
	err = os.MkdirAll(filepath.Dir(configPath), 0755)
	if err != nil {
		return fmt.Errorf("创建配置目录失败: %v", err)
	}

	// 写入文件
	err = ioutil.WriteFile(configPath, data, 0644)
	if err != nil {
		return fmt.Errorf("写入配置文件失败: %v", err)
	}

	fmt.Printf("已保存配置到文件: %s\n", configPath)
	return nil
}

// 从文件加载规则
func (a *App) loadRules() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 获取配置文件路径
	configPath := getConfigFilePath()
	fmt.Printf("正在尝试从 %s 加载配置...\n", configPath)

	// 检查文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Println("配置文件不存在，将使用默认空配置")
		return nil // 文件不存在，不处理
	}

	// 读取文件
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		fmt.Printf("读取配置文件失败: %v\n", err)
		return err
	}

	// 反序列化规则
	var loadedRules []SocksRule
	err = json.Unmarshal(data, &loadedRules)
	if err != nil {
		fmt.Printf("解析配置文件失败: %v\n", err)
		return err
	}

	// 添加ID如果不存在
	for i := range loadedRules {
		if loadedRules[i].ID == "" {
			loadedRules[i].ID = fmt.Sprintf("rule_%d", i+1)
		}
	}

	// 更新规则列表
	a.rules = loadedRules
	fmt.Printf("成功加载 %d 条规则\n", len(a.rules))

	return nil
}

// 获取配置文件的路径
func getConfigFilePath() string {
	// 尝试使用当前目录
	exePath, err := os.Executable()
	if err != nil {
		fmt.Printf("获取可执行文件路径失败: %v, 将使用当前目录\n", err)
		return configFile
	}

	exeDir := filepath.Dir(exePath)
	return filepath.Join(exeDir, configFile)
}
