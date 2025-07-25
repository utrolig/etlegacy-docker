package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	paths := loadPaths()

	downloadMaps(cfg, paths)

	log.Printf("Server starting on port %d", cfg.MapPort)
	log.Printf("Hostname: %s", cfg.Hostname)
	log.Printf("Stats submission enabled: %t", cfg.StatsSubmit)
}
