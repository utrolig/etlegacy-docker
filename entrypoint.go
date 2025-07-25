package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	// Server settings
	Hostname     string `envconfig:"HOSTNAME" default:"ETL Docker Server"`
	MapPort      int    `envconfig:"MAP_PORT" default:"27960"`
	RedirectURL  string `envconfig:"REDIRECTURL" default:"https://dl.etl.lol/maps/et"`
	MaxClients   int    `envconfig:"MAXCLIENTS" default:"32"`
	StartMap     string `envconfig:"STARTMAP" default:"radar"`
	TimeoutLimit int    `envconfig:"TIMEOUTLIMIT" default:"1"`
	ServerConf   string `envconfig:"SERVERCONF" default:"legacy6"`
	SvTracker    string `envconfig:"SVTRACKER" default:""`
	MOTD         string `envconfig:"CONF_MOTD" default:""`

	// Passwords
	Password     string `envconfig:"PASSWORD" default:""`
	RconPassword string `envconfig:"RCONPASSWORD" default:""`
	RefPassword  string `envconfig:"REFPASSWORD" default:""`
	ScPassword   string `envconfig:"SCPASSWORD" default:""`

	// ETLTV
	SvAutoDemo     int    `envconfig:"SVAUTODEMO" default:"0"`
	EtltvMaxSlaves int    `envconfig:"SVETLTVMAXSLAVES" default:"2"`
	EtltvPassword  string `envconfig:"SVETLTVPASSWORD" default:"3tltv"`

	// Repository
	SettingsURL    string `envconfig:"SETTINGSURL" default:"https://github.com/Oksii/legacy-configs.git"`
	SettingsPAT    string `envconfig:"SETTINGSPAT" default:""`
	SettingsBranch string `envconfig:"SETTINGSBRANCH" default:"main"`
	// Stats API settings
	StatsSubmit           bool   `envconfig:"STATS_SUBMIT" default:"false"`
	StatsAPILog           bool   `envconfig:"STATS_API_LOG" default:"false"`
	StatsAPIToken         string `envconfig:"STATS_API_TOKEN" default:""`
	StatsAPIPath          string `envconfig:"STATS_API_PATH" default:"/legacy/homepath/legacy/stats/"`
	StatsAPIURLSubmit     string `envconfig:"STATS_API_URL_SUBMIT" default:""`
	StatsAPIURLMatchID    string `envconfig:"STATS_API_URL_MATCHID" default:""`
	StatsAPIOBituaries    bool   `envconfig:"STATS_API_OBITUARIES" default:"true"`
	StatsAPIMessageLog    bool   `envconfig:"STATS_API_MESSAGELOG" default:"true"`
	StatsAPIDamageStat    bool   `envconfig:"STATS_API_DAMAGESTAT" default:"true"`
	StatsAPIShoveStats    bool   `envconfig:"STATS_API_SHOVESTATS" default:"true"`
	StatsAPIObjStats      bool   `envconfig:"STATS_API_OBJSTATS" default:"true"`
	StatsAPIDumpJSON      bool   `envconfig:"STATS_API_DUMPJSON" default:"false"`
	StatsAPIMovementStats bool   `envconfig:"STATS_API_MOVEMENTSTATS" default:"true"`
	StatsAPIStanceStats   bool   `envconfig:"STATS_API_STANCESTATS" default:"true"`
	StatsAPIAltMapScripts bool   `envconfig:"STATS_API_ALTMAPSCRIPTS" default:"false"`
	StatsAPIForceRename   bool   `envconfig:"STATS_API_FORCERENAME" default:"false"`

	// Extra assets settings
	Assets    bool   `envconfig:"ASSETS" default:"false"`
	AssetsURL string `envconfig:"ASSETS_URL" default:""`

	// Tracker API settings
	Tracker            bool   `envconfig:"TRACKER" default:"false"`
	TrackerAPIEndpoint string `envconfig:"TRACKER_API_ENDPOINT" default:""`
	TrackerAPIToken    string `envconfig:"TRACKER_API_TOKEN" default:""`
	TrackerDebug       bool   `envconfig:"TRACKER_DEBUG" default:"false"`

	// Maps
	Maps string `envconfig:"MAPS" default:"adlernest:braundorf_b4"`
}

func loadConfig() (*Config, error) {
	var cfg Config
	err := envconfig.Process("", &cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

type GamePaths struct {
	GameBase     string
	SettingsBase string
	Etmain       string
	Legacy       string
	Home         string
}

func loadPaths() GamePaths {
	gameBase := filepath.Join("/", "legacy", "server")
	settingsBase := filepath.Join(gameBase, "settings")
	etmain := filepath.Join(gameBase, "etmain")
	legacy := filepath.Join(gameBase, "legacy")
	home := filepath.Join("/", "legacy", "homepath")

	return GamePaths{
		GameBase:     gameBase,
		SettingsBase: settingsBase,
		Etmain:       etmain,
		Legacy:       legacy,
		Home:         home,
	}
}

func downloadMaps(cfg *Config, paths GamePaths) error {
	if cfg.Maps == "" {
		return nil
	}

	mapArray := strings.Split(cfg.Maps, ":")
	var mapsToDownload []string

	for _, mapName := range mapArray {
		mapName = strings.TrimSpace(mapName)
		if mapName == "" {
			continue
		}

		mapFilePath := filepath.Join(paths.Etmain, mapName+".pk3")

		if _, err := os.Stat(mapFilePath); err == nil {
			continue
		}

		log.Printf("Checking map %s", mapName)
		localMapPath := filepath.Join("/maps", mapName+".pk3")

		if _, err := os.Stat(localMapPath); err == nil {
			log.Printf("Map %s is sourcable locally, copying into place", mapName)
			if err := copyFile(localMapPath, mapFilePath); err != nil {
				log.Printf("Failed to copy local map %s: %v", mapName, err)
			}
		} else {
			mapsToDownload = append(mapsToDownload, mapName)
		}
	}

	if len(mapsToDownload) > 0 {
		log.Printf("Attempting to download %d maps in parallel", len(mapsToDownload))
		return downloadMapsParallel(cfg, paths, mapsToDownload)
	}

	return nil
}

func downloadMapsParallel(cfg *Config, paths GamePaths, maps []string) error {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 30)

	for _, mapName := range maps {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			mapURL := fmt.Sprintf("%s/etmain/%s.pk3", cfg.RedirectURL, name)
			mapFilePath := filepath.Join(paths.Etmain, name+".pk3")

			if err := downloadFile(mapURL, mapFilePath); err != nil {
				log.Printf("Failed to download %s: %v", name, err)
				os.Remove(mapFilePath)
			}

			log.Printf("Downloaded %s successfully", mapName)
		}(mapName)
	}

	wg.Wait()
	return nil
}

func downloadFile(url, filepath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

func ensureDirectory(path string) error {
	return os.MkdirAll(path, 0755)
}

func safeCopy(src, dst string) error {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil // Source file doesn't exist, skip silently
	}
	return copyFile(src, dst)
}

func copyGameAssets(paths GamePaths) error {
	if err := ensureDirectory(filepath.Join(paths.Etmain, "mapscripts")); err != nil {
		return fmt.Errorf("failed to create mapscripts directory: %v", err)
	}
	if err := ensureDirectory(filepath.Join(paths.Legacy, "luascripts")); err != nil {
		return fmt.Errorf("failed to create luascripts directory: %v", err)
	}

	mapscriptsDir := filepath.Join(paths.Etmain, "mapscripts")
	if files, err := filepath.Glob(filepath.Join(mapscriptsDir, "*.script")); err == nil {
		for _, file := range files {
			os.Remove(file)
		}
	}

	if sourceMapscripts, err := filepath.Glob(filepath.Join(paths.SettingsBase, "mapscripts", "*.script")); err == nil {
		for _, mapscript := range sourceMapscripts {
			filename := filepath.Base(mapscript)
			dst := filepath.Join(mapscriptsDir, filename)
			if err := safeCopy(mapscript, dst); err != nil {
				log.Printf("Warning: failed to copy mapscript %s: %v", filename, err)
			}
		}
	}

	luaScriptsDir := filepath.Join(paths.Legacy, "luascripts")
	if sourceLuaScripts, err := filepath.Glob(filepath.Join(paths.SettingsBase, "luascripts", "*.lua")); err == nil {
		for _, luascript := range sourceLuaScripts {
			filename := filepath.Base(luascript)
			dst := filepath.Join(luaScriptsDir, filename)
			if err := safeCopy(luascript, dst); err != nil {
				log.Printf("Warning: failed to copy luascript %s: %v", filename, err)
			}
		}
	}

	if sourceTomlFiles, err := filepath.Glob(filepath.Join(paths.SettingsBase, "luascripts", "*.toml")); err == nil {
		for _, tomlfile := range sourceTomlFiles {
			filename := filepath.Base(tomlfile)
			dst := filepath.Join(luaScriptsDir, filename)
			if err := safeCopy(tomlfile, dst); err != nil {
				log.Printf("Warning: failed to copy toml file %s: %v", filename, err)
			}
		}
	}

	if sourceCommandMaps, err := filepath.Glob(filepath.Join(paths.SettingsBase, "commandmaps", "*.pk3")); err == nil {
		for _, commandmap := range sourceCommandMaps {
			filename := filepath.Base(commandmap)
			dst := filepath.Join(paths.Legacy, filename)
			if err := safeCopy(commandmap, dst); err != nil {
				log.Printf("Warning: failed to copy commandmap %s: %v", filename, err)
			}
		}
	}

	configsDir := filepath.Join(paths.Etmain, "configs")
	os.RemoveAll(configsDir)
	if err := ensureDirectory(configsDir); err != nil {
		return fmt.Errorf("failed to create configs directory: %v", err)
	}

	if sourceConfigs, err := filepath.Glob(filepath.Join(paths.SettingsBase, "configs", "*.config")); err == nil {
		for _, config := range sourceConfigs {
			filename := filepath.Base(config)
			dst := filepath.Join(configsDir, filename)
			if err := copyFile(config, dst); err != nil {
				log.Printf("Warning: failed to copy config %s: %v", filename, err)
			}
		}
	}

	return nil
}

func updateServerConfig(cfg *Config, paths GamePaths) error {
	srcPath := filepath.Join(paths.SettingsBase, "etl_server.cfg")
	dstPath := filepath.Join(paths.Etmain, "etl_server.cfg")

	if err := copyFile(srcPath, dstPath); err != nil {
		return fmt.Errorf("failed to copy server config: %v", err)
	}

	content, err := os.ReadFile(dstPath)
	if err != nil {
		return fmt.Errorf("failed to read server config: %v", err)
	}

	configStr := string(content)

	if cfg.Password != "" {
		configStr += "\nset g_needpass \"1\"\n"
	}

	replacements := map[string]string{
		"%CONF_HOSTNAME%":                cfg.Hostname,
		"%CONF_MAP_PORT%":                strconv.Itoa(cfg.MapPort),
		"%CONF_REDIRECTURL%":             cfg.RedirectURL,
		"%CONF_MAXCLIENTS%":              strconv.Itoa(cfg.MaxClients),
		"%CONF_STARTMAP%":                cfg.StartMap,
		"%CONF_TIMEOUTLIMIT%":            strconv.Itoa(cfg.TimeoutLimit),
		"%CONF_SERVERCONF%":              cfg.ServerConf,
		"%CONF_SVTRACKER%":               cfg.SvTracker,
		"%CONF_PASSWORD%":                cfg.Password,
		"%CONF_RCONPASSWORD%":            cfg.RconPassword,
		"%CONF_REFPASSWORD%":             cfg.RefPassword,
		"%CONF_SCPASSWORD%":              cfg.ScPassword,
		"%CONF_SVAUTODEMO%":              strconv.Itoa(cfg.SvAutoDemo),
		"%CONF_ETLTVMAXSLAVES%":          strconv.Itoa(cfg.EtltvMaxSlaves),
		"%CONF_ETLTVPASSWORD%":           cfg.EtltvPassword,
		"%CONF_SETTINGSURL%":             cfg.SettingsURL,
		"%CONF_SETTINGSPAT%":             cfg.SettingsPAT,
		"%CONF_SETTINGSBRANCH%":          cfg.SettingsBranch,
		"%CONF_STATS_SUBMIT%":            boolToString(cfg.StatsSubmit),
		"%CONF_STATS_API_LOG%":           boolToString(cfg.StatsAPILog),
		"%CONF_STATS_API_TOKEN%":         cfg.StatsAPIToken,
		"%CONF_STATS_API_PATH%":          cfg.StatsAPIPath,
		"%CONF_STATS_API_URL_SUBMIT%":    cfg.StatsAPIURLSubmit,
		"%CONF_STATS_API_URL_MATCHID%":   cfg.StatsAPIURLMatchID,
		"%CONF_STATS_API_OBITUARIES%":    boolToString(cfg.StatsAPIOBituaries),
		"%CONF_STATS_API_MESSAGELOG%":    boolToString(cfg.StatsAPIMessageLog),
		"%CONF_STATS_API_DAMAGESTAT%":    boolToString(cfg.StatsAPIDamageStat),
		"%CONF_STATS_API_SHOVESTATS%":    boolToString(cfg.StatsAPIShoveStats),
		"%CONF_STATS_API_OBJSTATS%":      boolToString(cfg.StatsAPIObjStats),
		"%CONF_STATS_API_DUMPJSON%":      boolToString(cfg.StatsAPIDumpJSON),
		"%CONF_STATS_API_MOVEMENTSTATS%": boolToString(cfg.StatsAPIMovementStats),
		"%CONF_STATS_API_STANCESTATS%":   boolToString(cfg.StatsAPIStanceStats),
		"%CONF_STATS_API_ALTMAPSCRIPTS%": boolToString(cfg.StatsAPIAltMapScripts),
		"%CONF_STATS_API_FORCERENAME%":   boolToString(cfg.StatsAPIForceRename),
		"%CONF_ASSETS%":                  boolToString(cfg.Assets),
		"%CONF_ASSETS_URL%":              cfg.AssetsURL,
		"%CONF_TRACKER%":                 boolToString(cfg.Tracker),
		"%CONF_TRACKER_API_ENDPOINT%":    cfg.TrackerAPIEndpoint,
		"%CONF_TRACKER_API_TOKEN%":       cfg.TrackerAPIToken,
		"%CONF_TRACKER_DEBUG%":           boolToString(cfg.TrackerDebug),
	}

	for placeholder, value := range replacements {
		configStr = strings.ReplaceAll(configStr, placeholder, value)
	}

	re := regexp.MustCompile(`%CONF_[A-Z_]*%`)
	configStr = re.ReplaceAllString(configStr, "")

	if cfg.MOTD != "" {
		re := regexp.MustCompile(`(?m)^set server_motd[0-9].*$`)
		configStr = re.ReplaceAllString(configStr, "")

		motdLines := strings.Split(strings.ReplaceAll(cfg.MOTD, "\\n", "\n"), "\n")

		var motdConfig strings.Builder
		for i := range 6 {
			if i < len(motdLines) {
				motdConfig.WriteString(fmt.Sprintf("set server_motd%d          \"%s\"\n", i, motdLines[i]))
			} else {
				motdConfig.WriteString(fmt.Sprintf("set server_motd%d          \"\"\n", i))
			}
		}

		hostnameRe := regexp.MustCompile(`(?m)^set sv_hostname.*$`)
		configStr = hostnameRe.ReplaceAllStringFunc(configStr, func(match string) string {
			return match + "\n" + motdConfig.String()
		})
	}

	extraConfigPath := filepath.Join(paths.GameBase, "extra.cfg")
	if _, err := os.Stat(extraConfigPath); err == nil {
		extraContent, err := os.ReadFile(extraConfigPath)
		if err == nil {
			configStr += "\n" + string(extraContent)
		}
	}

	return os.WriteFile(dstPath, []byte(configStr), 0644)
}

func handleExtraContent(cfg *Config, paths GamePaths) error {
	if !cfg.Assets {
		return nil
	}

	log.Println("Downloading assets...")

	resp, err := http.Get(cfg.AssetsURL)
	if err != nil {
		log.Printf("Failed to download assets: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to download assets: HTTP %s", resp.Status)
		return fmt.Errorf("HTTP %s", resp.Status)
	}

	filename := filepath.Base(cfg.AssetsURL)
	if filename == "" || filename == "." {
		filename = "assets.zip"
	}

	assetPath := filepath.Join(paths.Legacy, filename)
	out, err := os.Create(assetPath)
	if err != nil {
		log.Printf("Failed to create asset file: %v", err)
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		log.Printf("Failed to write asset file: %v", err)
		return err
	}

	log.Printf("Downloaded assets to %s", assetPath)
	return nil
}

func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	paths := loadPaths()

	downloadMaps(cfg, paths)

	if err := copyGameAssets(paths); err != nil {
		log.Printf("Error copying game assets: %v", err)
	}

	if err := updateServerConfig(cfg, paths); err != nil {
		log.Printf("Error updating server config: %v", err)
	} else {
		log.Printf("Wrote server config successfully")
	}

	if err := handleExtraContent(cfg, paths); err != nil {
		log.Printf("Error handling extra content: %v", err)
	}

	log.Printf("Server starting on port %d", cfg.MapPort)
	log.Printf("Hostname: %s", cfg.Hostname)
	log.Printf("Stats submission enabled: %t", cfg.StatsSubmit)
}
