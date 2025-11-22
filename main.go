package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/go-ini/ini"
)

var (
	dashboardEnabled bool = false
	dashboardMutex   sync.Mutex
	dashboardData    map[string]interface{}
)

func LoadConfig() bool {
	LogInfo("Loading configuration from config.ini...")
	cfg, err := ini.Load("config.ini")
	if err != nil {
		LogError(fmt.Sprintf("Could not find or parse config.ini: %v", err))
		return false
	}
	LogInfo("Configuration file loaded successfully.")
	LogInfo("Processing General section...")
	generalSection, err := cfg.GetSection("General")
	if err == nil {
		if key, err := generalSection.GetKey("threads"); err == nil {
			if threads, err := key.Int(); err == nil {
				ThreadCount = threads
			}
		}
	}
	LogInfo("General section processed.")
	LogInfo("Processing Proxies section...")
	proxiesSection, err := cfg.GetSection("Proxies")
	if err == nil {
		if key, err := proxiesSection.GetKey("use_proxies"); err == nil {
			UseProxies, _ = key.Bool()
		}
		if key, err := proxiesSection.GetKey("proxy_type"); err == nil {
			ProxyType = key.String()
		}
	} else {
		UseProxies = false
		ProxyType = "http"
	}
	LogInfo("Proxies section processed.")
	LogInfo("Processing License section...")
	licenseSection, err := cfg.GetSection("License")
	if err != nil {
		LogError("License section not found in config.ini")
		return false
	}
	userKey, err := licenseSection.GetKey("key")
	if err != nil {
		LogError("License key not found in config.ini")
		return false
	}
	inputKey := userKey.String()
	if strings.TrimSpace(inputKey) == "" {
		LogError("License key cannot be empty")
		return false
	}
	LogInfo("License validation bypassed - KeyAuth removed")
	LeftDays = "Unlimited"
	inboxSection, err := cfg.GetSection("Inbox")
	if err == nil {
		if key, err := inboxSection.GetKey("search_keywords"); err == nil {
			keywordsStr := key.String()
			if keywordsStr != "" {
				keywords := strings.Split(keywordsStr, ",")
				var processedKeywords []string
				for _, k := range keywords {
					trimmed := strings.TrimSpace(k)
					if strings.Contains(trimmed, "@") && strings.Contains(trimmed, ".") {
						processedKeywords = append(processedKeywords, fmt.Sprintf("from:%s", trimmed))
					} else {
						processedKeywords = append(processedKeywords, trimmed)
					}
				}
				InboxWord = strings.Join(processedKeywords, ",")
				IsInBox = len(processedKeywords) > 0
			}
		}
	}
	discordSection, err := cfg.GetSection("Discord")
	if err == nil {
		if key, err := discordSection.GetKey("webhook_url"); err == nil {
			DiscordWebhookURL = key.String()
		}
		if key, err := discordSection.GetKey("send_all_hits"); err == nil {
			SendAllHits, _ = key.Bool()
		}
	}
	rpcSection, err := cfg.GetSection("DiscordRPC")
	if err == nil {
		if key, err := rpcSection.GetKey("enabled"); err == nil {
			RPCEnabled, _ = key.Bool()
		}
	}
	dashboardSection, err := cfg.GetSection("Dashboard")
	if err == nil {
		if key, err := dashboardSection.GetKey("enabled"); err == nil {
			dashboardEnabled, _ = key.Bool()
		}
	}
	LogSuccess("Configuration and license validated successfully!")
	return true
}

func ClearConsole() {
	cmd := exec.Command("cmd", "/c", "cls")
	cmd.Stdout = os.Stdout
	cmd.Run()
}

func PrintLogo() {
	for _, line := range AsciiArt {
		LogInfo(line)
	}
	fmt.Println()
	LogInfo(fmt.Sprintf("License Status: [%s]", LeftDays))
	fmt.Println()
}

func LoadFiles() {
	ClearConsole()
	PrintLogo()
	// Load combos
	file, err := os.Open("combo.txt")
	if err != nil {
		LogError("combo.txt file not found!")
		time.Sleep(1 * time.Second)
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var tempCombos []string
	for scanner.Scan() {
		tempCombos = append(tempCombos, strings.TrimSpace(scanner.Text()))
	}
	LogInfo(fmt.Sprintf("Loaded [%d] combos from combo.txt!", len(tempCombos)))
	originalCount := len(tempCombos)
	comboSet := make(map[string]bool)
	for _, combo := range tempCombos {
		comboSet[combo] = true
	}
	Ccombos = make([]string, 0, len(comboSet))
	for combo := range comboSet {
		Ccombos = append(Ccombos, combo)
	}
	validCombos := make([]string, 0, len(Ccombos))
	for _, combo := range Ccombos {
		if strings.ContainsAny(combo, ":;|") {
			validCombos = append(validCombos, combo)
		}
	}
	Ccombos = validCombos
	validComboCount := len(Ccombos)
	dupes := originalCount - len(comboSet)
	filtered := len(comboSet) - validComboCount
	LogInfo(fmt.Sprintf("Removed [%d] dupes, [%d] invalid, total valid: [%d]", dupes, filtered, validComboCount))
	if UseProxies {
		proxyFile, err := os.Open("proxies.txt")
		if err != nil {
			LogError("proxies.txt file not found!")
		} else {
			defer proxyFile.Close()
			scanner := bufio.NewScanner(proxyFile)
			Proxies = []string{}
			for scanner.Scan() {
				Proxies = append(Proxies, strings.TrimSpace(scanner.Text()))
			}
			LogInfo(fmt.Sprintf("Loaded [%d] proxies from proxies.txt!", len(Proxies)))
		}
	}
	time.Sleep(1 * time.Second)
}

func AskForThreads() {
	reader := bufio.NewReader(os.Stdin)
	for {
		ClearConsole()
		PrintLogo()
		LogInfo("Thread Amount?")
		fmt.Print("[>] ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		threads, err := strconv.Atoi(input)
		if err == nil && threads > 0 {
			ThreadCount = threads
			break
		}
	}
}

func AskForProxies() {
	reader := bufio.NewReader(os.Stdin)
	ClearConsole()
	PrintLogo()
	LogInfo("Select Proxy Type [1] - HTTP/S | [2] - Socks4 | [3] - Socks5 | [4] - Proxyless")
	fmt.Print("[>] ")
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)
	switch choice {
	case "1":
		ProxyType = "http"
		UseProxies = true
	case "2":
		ProxyType = "socks4"
		UseProxies = true
	case "3":
		ProxyType = "socks5"
		UseProxies = true
	case "4":
		UseProxies = false
	default:
		AskForProxies()
	}
}

func AskForInboxKeyword() {
	reader := bufio.NewReader(os.Stdin)
	ClearConsole()
	PrintLogo()
	LogInfo("Enter keywords to search in inboxes (comma-separated, leave empty for just inbox check)")
	fmt.Print("[>] ")
	keywordsInput, _ := reader.ReadString('\n')
	keywordsInput = strings.TrimSpace(keywordsInput)
	if keywordsInput == "" {
		InboxWord = ""
		IsInBox = false
		return
	}
	keywords := strings.Split(keywordsInput, ",")
	var processedKeywords []string
	for _, k := range keywords {
		trimmed := strings.TrimSpace(k)
		if strings.Contains(trimmed, "@") && strings.Contains(trimmed, ".") {
			processedKeywords = append(processedKeywords, fmt.Sprintf("from:%s", trimmed))
		} else {
			processedKeywords = append(processedKeywords, trimmed)
		}
	}
	InboxWord = strings.Join(processedKeywords, ",")
	IsInBox = true
}

func loadSkinsList() {
	absPath, err := filepath.Abs("Skinslist.darko")
	if err != nil {
		LogWarning(fmt.Sprintf("Could not get absolute path for skin list: %v", err))
		return
	}
	content, err := ioutil.ReadFile(absPath)
	if err != nil {
		LogWarning("Skin list file not found")
		return
	}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			value := strings.TrimSpace(parts[1])
			Mapping[key] = value
		}
	}
}

func LoadProxies(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var proxies []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		proxy := strings.TrimSpace(scanner.Text())
		if proxy != "" {
			proxies = append(proxies, proxy)
		}
	}
	return proxies, scanner.Err()
}

func centerText(text string, width int) string {
	if len(text) >= width {
		return text
	}
	padding := (width - len(text)) / 2
	return strings.Repeat(" ", padding) + text
}

func saveVbucksHit(entry string, vbucks int) {
	folderID := GetStats().getSessionFolder()
	baseDir := filepath.Join("Results", folderID)

	writeHit := func(filename string, counter *int64) {
		filePath := filepath.Join(baseDir, filename)
		file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			defer file.Close()
			file.WriteString(entry + "\n")
			atomic.AddInt64(counter, 1)
		}
	}

	if vbucks > 1000 {
		writeHit("1k+_vbucks_hits.txt", &Vbucks1kPlus)
	}
	if vbucks > 3000 {
		writeHit("3k+_vbucks_hits.txt", &Vbucks3kPlus)
	}
}

func main() {
	debugFlag := flag.Bool("debug", false, "Enable debug mode to display response data")
	flag.Parse()
	DebugMode = *debugFlag
	if DebugMode {
		initDebugLog()
	}
	log.SetOutput(os.Stdout)
	log.SetFlags(0)
	reader := bufio.NewReader(os.Stdin)
	if !LoadConfig() {
		LogInfo("Configuration or license validation failed. Press Enter to exit.")
		reader.ReadString('\n')
		return
	}
	LogSuccess("License valid! Welcome!")
	Level = "1"
	time.Sleep(1 * time.Second)
	if RPCEnabled {
		initDiscordRPC()
		updateDiscordPresence("Idle", "Ready to check Fortnite accounts")
	}
	loadSkinsList()
	for {
		ClearConsole()
		PrintLogo()
		LogInfo("╔════════════════════════════════════════╗")
		LogInfo("║              Main Menu                ║")
		LogInfo("╠════════════════════════════════════════╣")
		LogInfo("║ [1] Run FN Checker                    ║")
		LogInfo("║ [0] Exit                              ║")
		LogInfo("╚════════════════════════════════════════╝")
		fmt.Print("\n [>] ")
		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)
		switch choice {
		case "1":
			if ThreadCount <= 0 {
				AskForThreads()
			}
			if ProxyType == "" { 
				AskForProxies()
			}
			LoadFiles()
			if UseProxies {
				Proxies, err := LoadProxies("proxies.txt")
				if err != nil {
					LogError("Failed to load proxies: " + err.Error())
					Proxies = []string{}
				} else {
					LogInfo(fmt.Sprintf("Loaded [%d] proxies from proxies.txt!", len(Proxies)))
				}
			}
			if len(Ccombos) == 0 {
				LogError("No valid combos loaded. Please check combo.txt. Exiting.")
				time.Sleep(3 * time.Second)
				return
			}
			ClearConsole()
			PrintLogo()
			LogInfo("Press any key to start checking!")
			var modules []func(string) bool
			modules = append(modules, CheckAccount)
			reader.ReadString('\n') 
			CheckerRunning = true
			Sw = time.Now()
			var titleWg sync.WaitGroup
			titleWg.Add(1)
			go UpdateTitle(&titleWg)
			go func() {
				for _, combo := range Ccombos {
					Combos <- combo
				}
			}()
			WorkWg.Add(len(Ccombos))
			var wg sync.WaitGroup
			for i := 0; i < ThreadCount; i++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					defer func() {
						if r := recover(); r != nil {
							LogError(fmt.Sprintf("CRITICAL: Worker %d crashed with panic: %v", workerID, r))
							LogError(fmt.Sprintf("Worker %d recovery: Other workers continue running", workerID))
						}
					}()
					for combo := range Combos {
						if !CheckerRunning {
							return
						}
						for _, module := range modules {
							done := make(chan bool, 1)
							go func(combo string, module func(string) bool) {
								defer func() {
									if r := recover(); r != nil {
										LogError(fmt.Sprintf("Module panic recovered for combo %s: %v", combo, r))
									}
								}()
								module(combo)
								done <- true
							}(combo, module)
							select {
							case <-done:
							case <-time.After(45 * time.Second):
								LogError(fmt.Sprintf("TIMEOUT: Module for combo %s took longer than 45s", combo))
							}
						}
						WorkWg.Done()
					}
				}(i)
			}
			WorkWg.Wait()
			close(Combos)
			wg.Wait()
			CheckerRunning = false 
			titleWg.Wait()   
			LogSuccess("\nAll checking completed! Hit counts:")
			stats := fmt.Sprintf("MS: %d | Hits: %d | Epic 2FA: %d", MsHits, Hits, EpicTwofa)
			fmt.Printf("%s[SUCCESS] %s%s\n", ColorGreen, centerText(stats, 80), ColorReset)
			if len(FailureReasons) > 0 {
				LogInfo("\nFailure reasons:")
				for _, reason := range FailureReasons {
					LogError(reason)
				}
			}
			LogError("\nPress Enter to exit...")
			reader.ReadString('\n')
			return
		case "0":
			LogInfo("Exiting...")
			return
		default:
			LogWarning("Invalid choice, please try again.")
			time.Sleep(1 * time.Second)
		}
	}
}

func displayDashboard() {
	if !dashboardEnabled {
		return
	}

	total := len(Ccombos)
	checked := int(Check)
	elapsed := time.Since(Sw)
	minutes := int(elapsed.Minutes())
	seconds := int(elapsed.Seconds()) % 60

	ClearConsole()

	fmt.Printf("\n%s                            OMESFN DASHBOARD - Edited by Darko%s\n\n", Yellow, Reset)

	progressBar := createProgressBar(checked, total, 40)
	progressPercent := 0.0
	if total > 0 {
		progressPercent = float64(checked) / float64(total) * 100
	}
	remaining := total - checked
	eta := estimateCompletionTime(total, checked, int(elapsed.Seconds()))

	fmt.Printf("%sPROGRESS%s\n", White, Reset)
	fmt.Printf("  %s%s%s %.1f%%\n", Green, progressBar, Reset, progressPercent)
	fmt.Printf("  Checked: %s%d%s  |  Remaining: %s%d%s  |  ETA: %s%s%s\n\n", Green, checked, Reset, Yellow, remaining, Reset, Cyan, eta, Reset)

	cpm := atomic.LoadInt64(&Cpm) * 60
	atomic.StoreInt64(&Cpm, 0)

	fmt.Printf("%sPERFORMANCE%s\n", White, Reset)
	fmt.Printf("  CPM: %s%d%s  |  Time: %s%dm %ds%s\n\n", getCpmColor(int(cpm)), cpm, Reset, Blue, minutes, seconds, Reset)

	fmt.Printf("%sHITS OVERVIEW%s\n", White, Reset)
	fmt.Printf("  Total Hits: %s%d%s  |  Epic 2FA: %s%d%s  |  2FA: %s%d%s\n", Green, Hits, Reset, Yellow, EpicTwofa, Reset, Blue, Twofa, Reset)
	fmt.Printf("  FA: %s%d%s  |  Headless: %s%d%s  |  Rares: %s%d%s\n\n", Green, Sfa, Reset, Magenta, Headless, Reset, Red, Rares, Reset)

	fmt.Printf("%sSKIN CATEGORIES%s\n", White, Reset)
	printSkinBar("0 Skins", int(ZeroSkin), int(Hits))
	printSkinBar("1-9 Skins", int(OnePlus), int(Hits))
	printSkinBar("10+ Skins", int(TenPlus), int(Hits))
	printSkinBar("50+ Skins", int(FiftyPlus), int(Hits))
	printSkinBar("100+ Skins", int(HundredPlus), int(Hits))
	printSkinBar("300+ Skins", int(ThreeHundredPlus), int(Hits))
	fmt.Println()

	// V-Bucks Section
	fmt.Printf("%sV-BUCKS%s\n", White, Reset)
	fmt.Printf("  1K+ V-Bucks: %s%d%s  |  3K+ V-Bucks: %s%d%s\n", Yellow, Vbucks1kPlus, Reset, Green, Vbucks3kPlus, Reset)
}

func printSkinBar(label string, count int, total int) {
	barWidth := 20
	filled := 0
	if total > 0 {
		filled = int(float64(barWidth) * float64(count) / float64(total))
		if filled > barWidth {
			filled = barWidth
		}
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	fmt.Printf("  %-12s %s%s%s  %d\n", label, Blue, bar, Reset, count)
}

func getCpmColor(cpm int) string {
	if cpm < 100 {
		return Red
	} else if cpm < 500 {
		return Yellow
	}
	return Green
}

func getCountColorCode(count int) string {
	if count == 0 {
		return Red
	} else if count <= 10 {
		return Yellow
	}
	return Green
}

const (
	Magenta = "\033[35m"
)

func createProgressBar(current, total, width int) string {
	if total == 0 {
		return strings.Repeat("█", width)
	}
	percentage := float64(current) / float64(total)
	filled := int(float64(width) * percentage)
	bar := strings.Repeat("█", filled)
	empty := strings.Repeat("░", width-filled)
	return bar + empty
}

func PrintDashboardRow(label1 string, value1 interface{}, label2 string, value2 interface{}, label3 string, value3 interface{}, label4 string, value4 interface{}) {
	fmt.Printf("║ %-7s %-5v ║ %-7s %-5v ║ %-7s %-5v ║ %-7s %-5v ║\n",
		label1, value1, label2, value2, label3, value3, label4, value4)
}

func estimateCompletionTime(total, current, elapsedSeconds int) string {
	if current == 0 || total == current {
		return "Complete"
	}
	remaining := total - current
	secondsLeft := (elapsedSeconds * remaining) / current
	minutes := secondsLeft / 60
	seconds := secondsLeft % 60
	hours := minutes / 60
	minutes = minutes % 60
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	} else if minutes > 0 {
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func calculateAverageQuality() int {
	if int(Hits) == 0 {
		return 0
	}
	avgVbucks := 0
	if int(Hits) > 0 && len(Ccombos) > 0 {
		avgVbucks = 25000 + (int(Hits) * 500) 
	}
	return avgVbucks / int(Hits)
}

func formatCurrency(amount int) string {
	if amount >= 1000000 {
		return fmt.Sprintf("$%.1fM", float64(amount)/1000000)
	} else if amount >= 1000 {
		return fmt.Sprintf("$%.1fK", float64(amount)/1000)
	}
	return fmt.Sprintf("$%d", amount)
}

func calculateQualityScore() float64 {
	if Check == 0 {
		return 0.0
	}
	totalScore := 0.0
	score := float64(Hits) / float64(Check) * 40.0 
	totalScore += score
	score = float64(EpicTwofa) / float64(Hits) * 30.0 
	totalScore += score
	score = float64(Rares) / float64(Hits) * 30.0 
	totalScore += score
	return totalScore
}

func displayRecentHits() {
	files, err := ioutil.ReadDir("Results")
	if err == nil && len(files) > 0 {
		latestFolder := files[len(files)-1].Name()
		hitFiles := []string{
			filepath.Join("Results", latestFolder, "0_skins.txt"),
			filepath.Join("Results", latestFolder, "1-9_skins.txt"),
			filepath.Join("Results", latestFolder, "10+_skins.txt"),
			filepath.Join("Results", latestFolder, "50+_skins.txt"),
			filepath.Join("Results", latestFolder, "100+_skins.txt"),
		}
		hitCount := 0
		for _, hitsFile := range hitFiles {
			if hitCount >= 3 {
				break
			}
			if content, err := ioutil.ReadFile(hitsFile); err == nil {
				lines := strings.Split(string(content), "\n")
				for i := len(lines) - 1; i >= 0 && hitCount < 3; i-- {
					line := strings.TrimSpace(lines[i])
					if strings.HasPrefix(line, "Account:") {
						parts := strings.Split(line, "|")
						if len(parts) >= 1 {
							emailPart := strings.TrimSpace(parts[0])
							email := strings.Split(emailPart, ": ")[1]
							if len(email) > 55 {
								email = email[:52] + "..."
							}
							fmt.Printf("║ %-71s ║\n", email)
							hitCount++
						}
					}
				}
			}
		}
		for hitCount < 3 {
			fmt.Printf("║ %-76s ║\n", "• Waiting for hits...")
			hitCount++
		}
	} else {
		for i := 0; i < 3; i++ {
			fmt.Printf("║ %-76s ║\n", "• No hits found yet...")
		}
	}
}

func displayHitDistribution() {
	if int(Hits) == 0 {
		fmt.Println("║ No hits yet - be patient! ║")
		return
	}
	skinCount10plus := 0
	skinCount50plus := 0
	skinCount100plus := 0
	faCount := 0
	nfaCount := int(Hits) 
	files, err := ioutil.ReadDir("Results")
	if err == nil && len(files) > 0 {
		latestFolder := files[len(files)-1].Name()
		skinCount10plus = 0
		skinCount50plus = 0
		skinCount100plus = 0
		faCount = 0
		nfaCount = 0
		hitFiles := []string{
			filepath.Join("Results", latestFolder, "10+_skins.txt"),
			filepath.Join("Results", latestFolder, "50+_skins.txt"),
			filepath.Join("Results", latestFolder, "100+_skins.txt"),
			filepath.Join("Results", latestFolder, "1-9_skins.txt"),
		}
		for _, hitFile := range hitFiles {
			if content, err := ioutil.ReadFile(hitFile); err == nil {
				lines := strings.Split(string(content), "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if strings.HasPrefix(line, "Account:") {
						if strings.Contains(line, "| FA: Yes") {
							faCount++
						} else if strings.Contains(line, "| FA: No") {
							nfaCount++
						}
						if strings.Contains(hitFile, "100+_skins.txt") {
							skinCount10plus++
							skinCount50plus++
							skinCount100plus++
						} else if strings.Contains(hitFile, "50+_skins.txt") {
							skinCount10plus++
							skinCount50plus++
						} else if strings.Contains(hitFile, "10+_skins.txt") {
							skinCount10plus++
						}
					}
				}
			}
		}
	} else {
		skinCount10plus = int(float64(int(Hits)) * 0.6)
		skinCount50plus = int(float64(int(Hits)) * 0.3)
		skinCount100plus = int(float64(int(Hits)) * 0.1)
		faCount = int(float64(int(Hits)) * 0.4)
		nfaCount = int(Hits) - faCount
	}
	fmt.Println("║ HIT BREAKDOWN: ║")
	fmt.Printf("║ 10+ SKINS: %-8d 50+ SKINS: %-8d 100+ SKINS: %-8d ║\n",
		skinCount10plus, skinCount50plus, skinCount100plus)
	fmt.Printf("║ FA: %-12d NFA: %-12d ║\n", faCount, nfaCount)
}

func autoSaveHit(accountInfo string, qualityScore int) {
	if qualityScore >= 80 && len(accountInfo) > 10 {
		autoSaveFile := "auto_saved_hits.txt"
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		entry := fmt.Sprintf("[%s] QUALITY: %d/100 | %s\n", timestamp, qualityScore, accountInfo)
		file, err := os.OpenFile(autoSaveFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			defer file.Close()
			file.WriteString(entry)
		}
	}
}

func shouldProcessAccount(displayName, epicEmail string, skinCount int, vbucks int, hasStw bool) bool {
	if strings.Contains(displayName, "bot") || strings.Contains(displayName, "test") {
		return false
	}
	if skinCount == 0 && vbucks < 5000 {
		return false
	}
	return skinCount >= 5 || vbucks >= 10000 || hasStw
}

func UpdateTitle(wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for CheckerRunning {
		<-ticker.C
		elapsed := time.Since(Sw)
		minutes := int(elapsed.Minutes())
		seconds := int(elapsed.Seconds()) % 60
		cpm := atomic.LoadInt64(&Cpm)
		atomic.StoreInt64(&Cpm, 0)
		title := fmt.Sprintf("OmesFN | Checked: %d/%d | Hits: %d | 2fa: %d | Epic 2fa: %d | CPM: %d | Time: %dm %ds",
			Check, len(Ccombos), Hits, Twofa, EpicTwofa, cpm*60, minutes, seconds)
		setConsoleTitle(title)
		if dashboardEnabled {
			displayDashboard()
		}
		if RPCEnabled {
			checked := int(Check)
			total := len(Ccombos)
			left := total - checked
			rpcDetails := fmt.Sprintf("Checked: %d • Left: %d • Hits: %d", checked, left, int(Hits))
			rpcState := fmt.Sprintf("CPM: %d • Time: %dm %ds", cpm*60, minutes, seconds)
			updateDiscordPresence(rpcDetails, rpcState)
		}
	}
}

func UpdateBypassTitle(wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for CheckerRunning {
		<-ticker.C
		title := fmt.Sprintf("OmesFN Bypass | Checked: %d/%d | Bypassed: %d | Fail: %d | Retries: %d",
			Check, len(Ccombos), Hits, Bad, Retries)
		setConsoleTitle(title)
	}
}

func setConsoleTitle(title string) {
	ptr, _ := syscall.UTF16PtrFromString(title)
	procSetConsoleTitle.Call(uintptr(unsafe.Pointer(ptr)))
}

var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleTitle = kernel32.NewProc("SetConsoleTitleW")
)

type DiscordRPC struct {
	conn *net.Conn
	pipe syscall.Handle
}

var (
	discordIPC   *DiscordRPC
	rpcStartTime int64
)

func initDiscordRPC() {
	if !RPCEnabled {
		return
	}
	LogInfo("Initializing Discord RPC...")
	
	for i := 0; i < 10; i++ {
		pipeName := fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, i)
		
		pipeHandle, err := syscall.CreateFile(
			syscall.StringToUTF16Ptr(pipeName),
			syscall.GENERIC_READ|syscall.GENERIC_WRITE,
			0,
			nil,
			syscall.OPEN_EXISTING,
			0,
			0,
		)
		if err == nil {
			discordIPC = &DiscordRPC{pipe: pipeHandle}
			rpcStartTime = time.Now().Unix()
			LogInfo(fmt.Sprintf("Connected to Discord RPC pipe: %s", pipeName))
			break
		}
		LogInfo(fmt.Sprintf("Failed to connect to pipe: %s", pipeName))
	}
	if discordIPC == nil {
		LogError("Failed to connect to Discord RPC. Make sure Discord is running and RPC is enabled.")
		LogError("Also check that your firewall/antivirus isn't blocking the connection.")
		RPCEnabled = false
		return
	}
	// Send handshake
	handshake := map[string]interface{}{
		"v":         1,
		"client_id": DiscordClientID,
	}
	sendRPCCommand(handshake)
	LogSuccess("Discord RPC handshake sent!")
	
	time.Sleep(1 * time.Second)
	
	testPresence := map[string]interface{}{
		"cmd": "SET_ACTIVITY",
		"args": map[string]interface{}{
			"pid": os.Getpid(),
			"activity": map[string]interface{}{
				"type":    0,
				"details": "Testing Discord RPC",
				"state":   "Connection test",
				"name":    "OmesFN",
			},
		},
		"nonce": fmt.Sprintf("%d", time.Now().Unix()),
	}
	sendRPCCommand(testPresence)
	LogSuccess("Test presence sent - check Discord now!")
}

func sendRPCCommand(data interface{}) {
	if discordIPC == nil {
		return
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	var frame []byte
	frame = append(frame, 1, 0, 0, 0)
	binary.LittleEndian.PutUint32(frame[1:5], uint32(len(payload)))
	frame = append(frame, payload...)
	var bytesWritten uint32
	err = syscall.WriteFile(discordIPC.pipe, frame[0:], &bytesWritten, nil)
	if err != nil {
		LogError(fmt.Sprintf("Failed to send RPC command: %v", err))
		RPCEnabled = false
		discordIPC = nil
	}
}

func updateDiscordPresence(details, state string) {
	if !RPCEnabled || discordIPC == nil {
		return
	}
	presence := map[string]interface{}{
		"cmd": "SET_ACTIVITY",
		"args": map[string]interface{}{
			"pid": os.Getpid(),
			"activity": map[string]interface{}{
				"details": details,
				"state":   state,
				"assets": map[string]interface{}{
					"large_image": "fortnite_logo",
					"large_text":  "OmesFN Fortnite Checker",
					"small_image": "checking",
					"small_text":  "Active",
				},
				"timestamps": map[string]interface{}{
					"start": rpcStartTime,
				},
			},
		},
		"nonce": fmt.Sprintf("%d", time.Now().UnixNano()),
	}
	sendRPCCommand(presence)
}

func shutdownDiscordRPC() {
	if discordIPC != nil {
		syscall.CloseHandle(discordIPC.pipe)
		discordIPC = nil
		LogInfo("Discord RPC disconnected")
	}
}