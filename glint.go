package main

import (
	"context"
	"errors"
	"fmt"
	"glint/ast"
	"glint/config"
	"glint/crawler"
	"glint/dbmanager"
	"glint/global"
	"glint/logger"
	"glint/model"
	"glint/nenet"
	"glint/netcomm"
	"glint/pkg/pocs/apperror"
	"glint/pkg/pocs/bigpwdattack"
	"glint/pkg/pocs/cmdinject"
	"glint/pkg/pocs/contentsearch"
	"glint/pkg/pocs/cors"
	"glint/pkg/pocs/crlf"
	"glint/pkg/pocs/cspnotimplement"
	"glint/pkg/pocs/csrf"
	"glint/pkg/pocs/directorytraversal"
	"glint/pkg/pocs/fileinclude"
	"glint/pkg/pocs/jsonp"
	lowsomething "glint/pkg/pocs/lowVuln"
	"glint/pkg/pocs/nmapSsl"
	"glint/pkg/pocs/parampoll"
	"glint/pkg/pocs/sql"
	"glint/pkg/pocs/ssrfcheck"
	"glint/pkg/pocs/upfile"
	"glint/pkg/pocs/weakpwd"
	"glint/pkg/pocs/xsschecker"
	"glint/pkg/pocs/xxe"
	"glint/plugin"
	"glint/proxy"
	"glint/util"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "embed"

	"github.com/thoas/go-funk"
	"github.com/urfave/cli/v2"
)

// var ConfirmSocket bool
// var UnconfirmSocket false
// "xss", "csrf", "cmdinject", "jsonp", "xxe", "crlf", "cors", "sql", "tls", "csp", "apperror", "dir_coss",
// ,

/*
 */
var DefaultPlugins = cli.NewStringSlice("xss", "cmdinject", "jsonp", "xxe", "crlf", "cors", "sql",
	"apperror", "dir_coss",
	"tls", "fileinclude", "ssrf", "upfile", "csp", "textmatch", "weakattack", "bigpwdattack", "php_deserialization",
	"cookie", "hsts", "xFrameopt") //"ssrf","csrf",  "weakattack", ,

var signalChan chan os.Signal
var ConfigpPath string
var Plugins cli.StringSlice
var WebSocket string
var Socket string

var GenerateCA bool
var Dbconect bool
var Configtype string
var IsStartProxyMode bool //是否开启半自动代理模式

//go:embed version
var Version string

type Task struct {
	TaskId  int
	HostIds []int

	XssSpider     nenet.Spider
	Targets       []*model.Request
	TaskConfig    config.TaskConfig
	PluginWg      sync.WaitGroup
	Plugins       []*plugin.Plugin
	Ctx           *context.Context //当前任务的现场
	Cancel        *context.CancelFunc
	lock          *sync.Mutex
	Dm            *dbmanager.DbManager
	ScartTime     time.Time
	EndTime       time.Time
	Rate          util.Rate
	InstallDb     bool
	Progress      float64
	DoStartSignal chan bool
	PliuginsMsg   chan map[string]interface{}
	stoppluginmsg chan struct{}
	Status        util.Status
	ScanType      int //扫描模式
}

type PluginOption struct {
	PluginName     string
	PluginId       plugin.Plugin_type
	Callback       plugin.PluginCallback
	ReqList        map[string]interface{}
	InstallDb      bool
	IsAllUrlEval   bool
	Percentage     float64
	Bpayloadbrower bool
	HttpsCert      string
	HttpsCertKey   string
	IsExportJson   bool
}

type tconfig struct {
	InstallDb     bool
	EnableCrawler bool
	ProxyPort     int64
	HttpsCert     string
	HttpsCertKey  string
}

func (t *Task) ClearData() {
	t.TaskId = 0
	t.HostIds = []int{}
	t.XssSpider = nenet.Spider{}
	t.Targets = []*model.Request{}
	t.TaskConfig = config.TaskConfig{}
	t.Plugins = []*plugin.Plugin{}
	t.Ctx = nil
	t.Cancel = nil
	t.lock = nil
	t.Dm = nil
	t.ScartTime = time.Time{}
	t.EndTime = time.Time{}
	t.Rate = util.Rate{}
	t.InstallDb = false
	t.Progress = 0
	t.DoStartSignal = nil
	t.PliuginsMsg = nil
	t.stoppluginmsg = nil
	t.Status = 0
	t.ScanType = 0
}

func main() {

	// go func() {
	// 	ip := "0.0.0.0:6060"
	// 	if err := http.ListenAndServe(ip, nil); err != nil {
	// 		fmt.Printf("start pprof failed on %s\n", ip)
	// 	}
	// }()

	util.Initgob()

	//logger.DebugEnable(true)

	author := cli.Author{
		Name:  "wrench",
		Email: "ljl260435988@gmail.com",
	}

	// PassiveProxy := cli.Author{
	// 	Name:  "passiveproxy",
	// 	Email: "ljl260435988@gmail.com",
	// }

	app := &cli.App{
		// UseShortOptionHandling: true,
		Name:      "glint",
		Usage:     "A web vulnerability scanners",
		UsageText: "glint [global options] url1 url2 url3 ... (must be same host)",
		Version:   Version, // "v0.1.2"
		Authors:   []*cli.Author{&author},
		Flags: []cli.Flag{
			//设置配置文件路径
			&cli.StringFlag{
				Name:        "config",
				Aliases:     []string{},
				Usage:       "Scan Profile, Example `-c itop_task.json`",
				Value:       config.DefaultConfigPath,
				Destination: &ConfigpPath,
			},
			//设置需要开启的插件
			&cli.StringSliceFlag{
				Name:        "plugin",
				Aliases:     []string{},
				Usage:       "Vulnerable Plugin, Example `--plugin xss csrf ..., The same moudle`",
				Value:       DefaultPlugins,
				Destination: &Plugins,
			},

			//设置websocket地址
			&cli.StringFlag{
				Name:        "websocket",
				Aliases:     []string{},
				Usage:       "Websocket Communication Address. Example `--websocket 127.0.0.1:8081`",
				Value:       config.DefaultSocket,
				Destination: &WebSocket,
			},

			//读取的config类型
			&cli.StringFlag{
				Name:        "configtype",
				Aliases:     []string{},
				Usage:       "Read Config file type. Example `--configtype json|yaml`",
				Value:       config.DefaultConfigType,
				Destination: &Configtype,
			},

			//设置socket地址
			&cli.StringFlag{
				Name:        "socket",
				Aliases:     []string{},
				Usage:       "socket Communication Address. Example `--socket 127.0.0.1:8081`",
				Value:       config.DefaultSocket,
				Destination: &Socket,
			},
			&cli.BoolFlag{
				Name:        "passiveproxy",
				Aliases:     []string{},
				Usage:       "start passiveproxy",
				Value:       false,
				Destination: &config.PassiveProxy,
			},
			&cli.BoolFlag{
				Name:        "generate-ca-cert",
				Aliases:     []string{},
				Usage:       "generate CA certificate and private key for MITM",
				Value:       false,
				Destination: &GenerateCA,
			},
			&cli.StringFlag{
				Name:        "cert",
				Aliases:     []string{},
				Usage:       "import certificate path",
				Value:       "",
				Destination: &Cert,
			},
			&cli.StringFlag{
				Name:        "key",
				Aliases:     []string{},
				Usage:       "import certificate private key path",
				Value:       "",
				Destination: &PrivateKey,
			},

			&cli.BoolFlag{
				Name:        "dbconnect",
				Aliases:     []string{},
				Usage:       "Wherever Database Connect",
				Value:       false,
				Destination: &Dbconect,
			},
		},
		Action: run,
	}
	err := app.Run(os.Args)
	if err != nil {
		logger.Error(err.Error())
	}

}

func run(c *cli.Context) error {
	// var req model.Request
	logger.DebugEnable(false)
	signalChan = make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	if strings.ToLower(WebSocket) != "" {
		WebSocketHandler()
	} else if strings.ToLower(Socket) != "" {
		SocketHandler()
	} else if config.PassiveProxy {
		// if c.Args().Len() == 0 {
		// 	logger.Error("url must be set")
		// 	return errors.New("url must be set")
		// }
		t := Task{TaskId: 9564}

		config := tconfig{}
		config.EnableCrawler = false
		config.InstallDb = false

		t.Init()
		CmdHandler(c, &t, config)
	} else {
		// go func() {
		// 	ip := "0.0.0.0:6061"
		// 	if err := http.ListenAndServe(ip, nil); err != nil {
		// 		fmt.Printf("start pprof failed on %s\n", ip)
		// 	}
		// }()

		if c.Args().Len() == 0 {
			logger.Error("url must be set")
			return errors.New("url must be set")
		}
		t := Task{TaskId: 9564}
		t.Init()
		config := tconfig{}
		config.EnableCrawler = true
		config.InstallDb = false

		CmdHandler(c, &t, config)

		errc := make(chan error, 1)
		// go func() {
		// 	errc <- s.Serve(l)
		// }()
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt)

		select {
		case err := <-errc:
			logger.Error("failed to serve: %v", err)
		case sig := <-sigs:
			logger.Error("terminating: %v", sig)
		}

	}
	return nil
}

func craw_cleanup(c *crawler.CrawlerTask) {
	if !c.Pool.IsClosed() {
		c.Pool.Tune(1)
		c.Pool.Release()
		c.Browser.Close()
	}
	c.Reset()
}

// func (t *Task) task_cleanup() {
// 	if len(t.Plugins) != 0 {
// 		for _, plugin := range t.Plugins {
// 			plugin.Pool.Tune(1)
// 			(*plugin.Cancel)()
// 			if plugin.Spider != nil {
// 				plugin.Spider.Close()
// 			}
// 		}
// 	}
// }

// 删除数据库内容
func (t *Task) deletedbresult() error {
	err := t.Dm.DeleteScanResult(t.TaskId)
	if err != nil {
		logger.Error(err.Error())
	}
	return err
}

func (t *Task) close() {
	//由外部socket关闭避免重复释放
	if _, ok := (*t.Ctx).Deadline(); !ok {
		(*t.Cancel)()
	}
	//删除插件
	if len(t.Plugins) != 0 {
		for _, plugin := range t.Plugins {
			plugin.Pool.Tune(1)
			if plugin.Cancel != nil {
				(*plugin.Cancel)()
			}
			if plugin.Spider != nil {
				plugin.Spider.Close()
				plugin.Spider = nil
			}
			plugin.Pool = nil
		}
	}
	//t.ClearData()
}

func (t *Task) setprog(progress float64) {
	// p := util.Decimal(progress)
	//t.lock.Lock()
	t.Progress += progress
	//t.lock.Unlock()
}

// 发送进度条到通知队列
func (t *Task) sendprog() {
	Element := make(map[string]interface{})
	Element["status"] = 1
	Element["progress"] = t.Progress

	select {
	case t.PliuginsMsg <- Element:
	case <-time.After(time.Second * 5):
	}

}

// packageType := reflect.TypeOf(plugin.PluginCallback)

// 一个脚本检测所有网页
func (t *Task) EnablePluginsByUri(
	originUrls map[string]interface{},
	// percentage float64,
	HttpsCert string,
	HttpsCertKey string,
	isexport bool,
	isSocket bool) {
	StartPlugins := Plugins.Value()
	percentage := 0.038
	for _, PluginName := range StartPlugins {
		switch strings.ToLower(PluginName) {
		case "tls":
			t.AddPlugins("TlS", plugin.TLS, nmapSsl.TLSv0verify, originUrls, isSocket, true, percentage, false, HttpsCert, HttpsCertKey, isexport)
			t.AddPlugins("TlS", plugin.TLS, nmapSsl.TLSv1verify, originUrls, isSocket, true, percentage, false, HttpsCert, HttpsCertKey, isexport)
			t.AddPlugins("TlS", plugin.TLS, nmapSsl.Sweet32verify, originUrls, isSocket, true, percentage, false, HttpsCert, HttpsCertKey, isexport)
			t.AddPlugins("TlS", plugin.TLS, nmapSsl.TlsWeakverify, originUrls, isSocket, true, percentage, false, HttpsCert, HttpsCertKey, isexport)
		case "csp":
			t.AddPlugins("CSP", plugin.CSP, cspnotimplement.CSPStartTest, originUrls, isSocket, true, percentage, false, HttpsCert, HttpsCertKey, isexport)
		case "apperror":
			t.AddPlugins("APPERROR", plugin.APPERROR, apperror.Application_startTest, originUrls, isSocket, true, percentage, false, HttpsCert, HttpsCertKey, isexport)
		case "textmatch":
			t.AddPlugins("CONTENTSEARCH", plugin.CONTENTSEARCH, contentsearch.Start_text_Macth, originUrls, isSocket, true, 0., false, HttpsCert, HttpsCertKey, isexport)
		}
	}

}

// 一个脚本检测一个网页
func (t *Task) EnablePluginsALLURL(
	originUrls map[string]interface{},
	// percentage float64,
	HttpsCert string,
	HttpsCertKey string,
	isexport bool,
	isSocket bool) {
	StartPlugins := Plugins.Value()
	percentage := 0.038
	for _, PluginName := range StartPlugins {
		switch strings.ToLower(PluginName) {
		case "csrf":
			t.AddPlugins("CSRF", plugin.Csrf, csrf.Csrfeval, originUrls, isSocket, false, percentage, false, HttpsCert, HttpsCertKey, isexport)
		case "xss":
			t.AddPlugins("XSS", plugin.Xss, xsschecker.CheckXss, originUrls, isSocket, false, percentage, true, HttpsCert, HttpsCertKey, isexport)
		case "ssrf":
			t.AddPlugins("SSRF", plugin.Ssrf, ssrfcheck.Ssrf, originUrls, isSocket, false, percentage, false, HttpsCert, HttpsCertKey, isexport)
		case "jsonp":
			t.AddPlugins("JSONP", plugin.Jsonp, jsonp.JsonpValid, originUrls, isSocket, false, percentage, false, HttpsCert, HttpsCertKey, isexport)
		case "cmdinject":
			t.AddPlugins("CMDINJECT", plugin.CmdInject, cmdinject.CmdValid, originUrls, isSocket, false, percentage, false, HttpsCert, HttpsCertKey, isexport)
		case "xxe":
			t.AddPlugins("XXE", plugin.Xxe, xxe.Xxe, originUrls, isSocket, false, 0., false, HttpsCert, HttpsCertKey, isexport)
		case "crlf":
			t.AddPlugins("CRLF", plugin.Crlf, crlf.Crlf, originUrls, isSocket, false, 0., false, HttpsCert, HttpsCertKey, isexport)
		case "cors":
			t.AddPlugins("CORS", plugin.CORS, cors.Cors_Valid, originUrls, isSocket, false, 0., false, HttpsCert, HttpsCertKey, isexport)
		case "sql":
			t.AddPlugins("SQL", plugin.SQL, sql.Sql_inject_Vaild, originUrls, isSocket, false, 0., false, HttpsCert, HttpsCertKey, isexport)
		case "dir_coss":
			t.AddPlugins("DIR_COSS", plugin.DIR_COSS, directorytraversal.TraversalVaild, originUrls, isSocket, false, 0., false, HttpsCert, HttpsCertKey, isexport)
		case "bigpwdattack":
			t.AddPlugins("BIGPWDATTACK", plugin.BigPwdAttack, bigpwdattack.StartTesting, originUrls, isSocket, false, 0., false, HttpsCert, HttpsCertKey, isexport)
		case "weakattack":
			t.AddPlugins("WEAKATTACK", plugin.WeakPwdAttack, weakpwd.StartTesting, originUrls, isSocket, false, 0., false, HttpsCert, HttpsCertKey, isexport)
		case "fileinclude":
			t.AddPlugins("FileINCLUDE", plugin.Fileinclude, fileinclude.FileincludeValid, originUrls, isSocket, false, 0., false, HttpsCert, HttpsCertKey, isexport)
		case "upfile":
			t.AddPlugins("UPFILE", plugin.UPFile, upfile.UpfileVaild, originUrls, isSocket, false, 0., false, HttpsCert, HttpsCertKey, isexport)
			// case "php_deserialization":
			// 	t.AddPlugins("Deserialization", plugin.Deserialization, deserialization.PHPDeserializaValid, originUrls, isSocket, false, 0., false, HttpsCert, HttpsCertKey, isexport)
		case "parampull":
			t.AddPlugins("Parampoll", plugin.ParamPoll, parampoll.StartTesting, originUrls, isSocket, false, 0., false, HttpsCert, HttpsCertKey, isexport)
		}
	}
}

// 只检测主域名
func (t *Task) EnablePluginsByDomain(
	originUrls map[string]interface{},
	// percentage float64,
	HttpsCert string,
	HttpsCertKey string,
	isexport bool,
	isSocket bool) {
	StartPlugins := Plugins.Value()
	percentage := 0.038
	for _, PluginName := range StartPlugins {
		switch strings.ToLower(PluginName) {
		case "cookie":
			t.AddPlugins("cookie", "rj-022-0001", lowsomething.Cookies_not_set_SameSite_flag, originUrls, isSocket, true, percentage, false, HttpsCert, HttpsCertKey, isexport)
			t.AddPlugins("cookie", "rj-022-0002", lowsomething.Cookies_not_set_httponly_flag, originUrls, isSocket, true, percentage, false, HttpsCert, HttpsCertKey, isexport)
			t.AddPlugins("cookie", "rj-022-0003", lowsomething.Cookies_not_set_secure_flag, originUrls, isSocket, true, percentage, false, HttpsCert, HttpsCertKey, isexport)
			//t.AddPlugins("cookie", plugin.Cookie_detection, lowsomething.TlsWeakverify, originUrls, isSocket, false, percentage, false, HttpsCert, HttpsCertKey, isexport)
		case "hsts":
			t.AddPlugins("hsts", plugin.HSTS_detection, lowsomething.Hsts__Valid, originUrls, isSocket, true, percentage, false, HttpsCert, HttpsCertKey, isexport)
		case "xFrameopt":
			t.AddPlugins("xFrameopt", plugin.X_Frame_Options, lowsomething.Jacking_X_Frame_Options_Valid, originUrls, isSocket, true, percentage, false, HttpsCert, HttpsCertKey, isexport)
		}

	}

}

// bpayloadbrower 该插件是否开启浏览器方式发送payload
func (t *Task) AddPlugins(
	PluginName string,
	PluginId plugin.Plugin_type,
	callback plugin.PluginCallback,
	ReqList map[string]interface{},
	installDb bool,
	isAllUrlEval bool,
	percentage float64,
	bpayloadbrower bool,
	HttpsCert string,
	HttpsCertKey string,
	isExportJson bool,

) {
	myfunc := []plugin.PluginCallback{}
	myfunc = append(myfunc, callback)
	var Payloadcarrier *nenet.Spider
	if bpayloadbrower {
		t.XssSpider.Ratelimite = &t.Rate
		Payloadcarrier = &t.XssSpider
	} else {
		Payloadcarrier = nil
	}

	pluginInternal := plugin.Plugin{
		PluginName:   PluginName,
		PluginId:     PluginId,
		MaxPoolCount: 5,
		Callbacks:    myfunc,
		InstallDB:    installDb,
		Spider:       Payloadcarrier,
		Taskid:       t.TaskId,
		Timeout:      time.Minute * 60,
		Progperc:     percentage,
		Dm:           t.Dm,
	}
	pluginInternal.Init()
	t.PluginWg.Add(1)
	//t.lock.Lock()
	t.Plugins = append(t.Plugins, &pluginInternal)
	//t.lock.Unlock()

	// AllowDomain, err := t.TaskConfig.GetValue("allow_domain")
	// if err != nil {
	// 	panic(err)
	// }

	// if IsSocket
	CopyReqList := util.CopyMap(ReqList)

	args := plugin.PluginOption{
		PluginWg:         &t.PluginWg,
		Progress:         &t.Progress,
		IsSocket:         installDb,
		Data:             CopyReqList,
		TaskId:           t.TaskId,
		SingelMsg:        &t.PliuginsMsg,
		Totalprog:        percentage,
		HttpsCert:        HttpsCert,
		HttpsCertKey:     HttpsCertKey,
		Rate:             &t.Rate,
		IsSaveToJsonFile: isExportJson,
		Config:           t.TaskConfig,
		IsAllUrlsEval:    isAllUrlEval,
	}

	go func(args *plugin.PluginOption) {
		pluginInternal.Run(*args)
		if pluginInternal.Pool.IsClosed() {
			logger.Warning("插件【%s】的线程池已关闭.", pluginInternal.PluginName)
		} else {
			logger.Error("插件【%s】的线程池未关闭.", pluginInternal.PluginName)
		}
	}(&args)
}

func (t *Task) CrawlerConvertToMap(
	Results []*crawler.Result,
	DATA1 *map[string][]interface{},
	DATA2 *map[string][]ast.JsonUrl,
	IscollectUri bool) {

	for _, result := range Results {
		funk.Map(result.ReqList, func(r *model.Request) bool {
			if IscollectUri {
				// //处理扫描深度，超过的跳出
				// if util.GetScanDeepByUrl(r.URL.String()) >= int(scan_depth.Int()) {
				// 	return false
				// }
				if r.URL.Hostname() == result.HOSTNAME {
					element0 := ast.JsonUrl{
						Url:     r.URL.String(),
						MetHod:  r.Method,
						Headers: r.Headers,
						Data:    r.PostData,
						Source:  r.Source,
						Hostid:  result.Hostid,
					}
					element := make(map[string]interface{}, 0)
					element["url"] = r.URL.String()
					element["method"] = r.Method
					element["headers"] = r.Headers
					element["data"] = r.PostData
					element["source"] = r.Source
					element["hostid"] = result.Hostid
					element["pagestate"] = r.PageState
					if DATA1 != nil {
						(*DATA1)[r.GroupsId] = append((*DATA1)[r.GroupsId], element)
					}
					if DATA2 != nil {
						(*DATA2)[r.GroupsId] = append((*DATA2)[r.GroupsId], element0)
					}
					return false
				}
			} else {
				element0 := ast.JsonUrl{
					Url:     r.URL.String(),
					MetHod:  r.Method,
					Headers: r.Headers,
					Data:    r.PostData,
					Source:  r.Source,
					Hostid:  result.Hostid,
				}
				element := make(map[string]interface{}, 0)
				element["url"] = r.URL.String()
				element["method"] = r.Method
				element["headers"] = r.Headers
				element["data"] = r.PostData
				element["source"] = r.Source
				element["hostid"] = result.Hostid
				element["pagestate"] = r.PageState
				if DATA1 != nil {
					(*DATA1)[r.GroupsId] = append((*DATA1)[r.GroupsId], element)
				}
				if DATA2 != nil {
					(*DATA2)[r.GroupsId] = append((*DATA2)[r.GroupsId], element0)
				}
			}
			return false
		})
	}
}

func CrawlerConvertToMap(
	Results []*crawler.Result,
	DATA1 *map[string][]interface{},
	DATA2 *map[string][]ast.JsonUrl,
	IscollectUri bool) {

	// scan_depth, err := t.TaskConfig.GetValue("scan_depth")
	// if err != nil {
	// 	panic(err)
	// }

	for _, result := range Results {
		funk.Map(result.ReqList, func(r *model.Request) bool {
			if IscollectUri {
				// //处理扫描深度，超过的跳出
				// if util.GetScanDeepByUrl(r.URL.String()) >= int(scan_depth.Int()) {
				// 	return false
				// }

				if r.URL.Hostname() == result.HOSTNAME {
					element0 := ast.JsonUrl{
						Url:     r.URL.String(),
						MetHod:  r.Method,
						Headers: r.Headers,
						Data:    r.PostData,
						Source:  r.Source,
						Hostid:  result.Hostid,
					}
					element := make(map[string]interface{})
					element["url"] = r.URL.String()
					element["method"] = r.Method
					element["headers"] = r.Headers
					element["data"] = r.PostData
					element["source"] = r.Source
					element["hostid"] = result.Hostid
					if DATA1 != nil {
						(*DATA1)[r.GroupsId] = append((*DATA1)[r.GroupsId], element)
					}
					if DATA2 != nil {
						(*DATA2)[r.GroupsId] = append((*DATA2)[r.GroupsId], element0)
					}
					return false
				}
			} else {
				element0 := ast.JsonUrl{
					Url:     r.URL.String(),
					MetHod:  r.Method,
					Headers: r.Headers,
					Data:    r.PostData,
					Source:  r.Source,
					Hostid:  result.Hostid,
				}
				element := make(map[string]interface{})
				element["url"] = r.URL.String()
				element["method"] = r.Method
				element["headers"] = r.Headers
				element["data"] = r.PostData
				element["source"] = r.Source
				element["hostid"] = result.Hostid
				if DATA1 != nil {
					(*DATA1)[r.GroupsId] = append((*DATA1)[r.GroupsId], element)
				}
				if DATA2 != nil {
					(*DATA2)[r.GroupsId] = append((*DATA2)[r.GroupsId], element0)
				}
			}
			return false
		})
	}
}

func (t *Task) dostartTasks(tconfig tconfig) error {
	var (
		err       error
		crawtasks []*crawler.CrawlerTask
		Results   []*crawler.Result

		Duts []crawler.DatabeseUrlTree
	)

	ALLURLS := make(map[string][]interface{}, 0)
	URLSList := make(map[string]interface{}, 0)
	ALLURI := make(map[string][]interface{}, 0)
	URISList := make(map[string]interface{}, 0)
	JSONALLURLS := make(map[string][]ast.JsonUrl, 0)

	// domain := make(map[string][]interface{}, 0)
	// element := make(map[string]interface{}, 0)
	// domainlist := make(map[string]interface{}, 0)

	if tconfig.InstallDb {
		t.deletedbresult()
		t.Dm.DeleteGrabUri(t.TaskId)
		userlist, _ := t.Dm.GetUserNameORPassword(int(t.TaskConfig.Yaml.User_dic_id))
		config.GlobalUserNameList = append(config.GlobalUserNameList, userlist...)
		passlist, _ := t.Dm.GetUserNameORPassword(int(t.TaskConfig.Yaml.Pwd_dic_id))
		config.GlobalPasswordList = append(config.GlobalPasswordList, passlist...)
	}
	//完成后通知上下文
	defer t.close()
	// defer t.task_cleanup()

	StartPlugins := Plugins.Value()
	percentage := 1 / float64(len(StartPlugins)+1)
	logger.Info("config.EnableCrawler: %v", tconfig.EnableCrawler)
	if tconfig.EnableCrawler {
		qps, err := t.TaskConfig.GetValue("qps")
		if err != nil {
			panic(err)
		}
		// qpsI, err := strconv.Atoi(qps.String())
		t.Rate.InitRate(uint(qps.Int()))

		for _, Target := range t.Targets {
			// if !util.Isdomainonline(Target.URL.String()) {
			// 	continue
			// }
			Crawtask, err := crawler.NewCrawlerTask(t.Ctx, Target, t.TaskConfig)
			Crawtask.Result.Hostid = Target.DomainId

			//是否通知socket消息,一般插入数据库默认为BS模式
			Crawtask.IsSocket = tconfig.InstallDb
			t.XssSpider.Init(t.TaskConfig)
			Crawtask.PluginBrowser = &t.XssSpider
			if err != nil {
				logger.Error(err.Error())
				return err
			}
			logger.Info("Start crawling.")
			Crawtask.Scandeep = int(t.TaskConfig.Yaml.ScanDepth)
			crawtasks = append(crawtasks, Crawtask)

			//Crawtask.Run()是同步函数
			go Crawtask.Run()
		}

		//等待爬虫结束
		for _, crawtask := range crawtasks {
			//这个是真正等待结束
			crawtask.Waitforsingle()

			result := crawtask.Result
			// result.Hostid = crawtask.Result.Hostid
			// result.HOSTNAME = crawtask.HostName
			// fmt.Printf("爬取 %s 域名结束", crawtask.HostName)

			result.Hostid = crawtask.Result.Hostid
			result.HOSTNAME = crawtask.HostName
			fmt.Printf("爬取 %s 域名结束", crawtask.HostName)

			// SiteRootNode.TaskId = int64(t.TaskId)
			// SiteRootNode.HostID = result.Hostid
			// for _, v := range result.ReqList {
			// 	SiteRootNode.ADD_NODE(v.URL.String())
			// }

			Results = append(Results, result)
			logger.Info(fmt.Sprintf("Task finished, %d results, %d requests, %d subdomains, %d domains found.",
				len(result.ReqList), len(result.AllReqList), len(result.SubDomainList), len(result.AllDomainList)))
			craw_cleanup(crawtask)
		}

		t.setprog(percentage)

		if tconfig.InstallDb {
			t.sendprog()
			GrapUrls := []dbmanager.GrapUrl{}
			for _, v := range Results {
				var SiteRootNode crawler.SiteRootNode
				SiteRootNode.HostID = v.Hostid
				SiteRootNode.TaskId = int64(t.TaskId)
				for _, r := range v.ReqList {
					SiteRootNode.ADD_NODE(r.URL.String())
					u := dbmanager.GrapUrl{Taskid: int64(t.TaskId), Hostid: v.Hostid, Url: r.URL.String()}
					GrapUrls = append(GrapUrls, u)
				}
				Duts = SiteRootNode.RootNodetoDBinfo(SiteRootNode.Root())
				t.Dm.SaveUrlTree(Duts)

			}

			// t.Dm.SaveGrabUri(GrapUrls)
			//保存URl树

		}

		t.CrawlerConvertToMap(Results, &ALLURLS, &JSONALLURLS, false)

		t.CrawlerConvertToMap(Results, &ALLURI, nil, true)

		ast.SaveCrawOutPut(JSONALLURLS, "result.json")

		for s, v := range ALLURLS {
			URLSList[s] = v
		}

		for s, v := range ALLURI {
			URISList[s] = v
		}

		t.TaskConfig.JsonOrYaml = true
		//Crawtask.PluginBrowser = t.XssSpider
		//爬完虫加载插件检测漏洞
		var issocket = false
		if tconfig.InstallDb {
			issocket = true
		}
		// //TEST
		// issocket = true

		if len(URISList) == 0 {
			goto quit
		}

		t.EnablePluginsALLURL(URISList, tconfig.HttpsCert, tconfig.HttpsCertKey, false, issocket)
		t.EnablePluginsByUri(URISList, tconfig.HttpsCert, tconfig.HttpsCertKey, false, issocket)
		t.EnablePluginsByDomain(URISList, tconfig.HttpsCert, tconfig.HttpsCertKey, false, issocket)

		t.PluginWg.Wait()
		//清空插件数据
		for _, pluginInternal := range t.Plugins {
			pluginInternal.ClearData()
		}

	quit:
		Taskslock.Lock()
		removetasks(t.TaskId)
		Taskslock.Unlock()
		if tconfig.InstallDb {
			t.SaveQuitTimeToDB()
		}

		// t.close()

		logger.Info("The End for task:%d", t.TaskId)
		runtime.GC()
	} else {
		//不开启爬虫启动被动代理模式
		//Ratelimite := util.Rate{}
		// if ok, err := util.ConfirmVlockFile("v-clock.lock"); !ok {
		// 	logger.Error("cpu校验失败,error:%s", err.Error())
		// 	// netcomm.Sendmsg(-1, "授权校验失败", t.TaskId)
		// }

		//设计内部通讯
		var m MConn
		IsStartProxyMode = true
		//errc := make(chan error, 1)
		m.Init()
		server_control, err := NewTaskServer("proxy")
		m.CallbackFunc = server_control.Task
		if err != nil {
			logger.Error(err.Error())
			return err
		}
		listener, err := net.Listen("tcp", "127.0.0.1:30986")
		if err != nil {
			logger.Error(err.Error())
			return err
		}

		defer listener.Close()
		go func() {
			for {
				con, err := listener.Accept()
				if err != nil {
					logger.Error(err.Error())
					continue
				}
				go m.Listen(con)
				netcomm.SOCKETCONN = append(netcomm.SOCKETCONN, &con)
			}
		}()

		qps, err := t.TaskConfig.GetValue("qps")
		if err != nil {
			panic(err)
		}

		qpsI, err := strconv.Atoi(qps.String())
		t.Rate.InitRate(uint(qpsI))
		s := SProxy{}
		s.CallbackFunc = t.agentPluginRun
		s.Run()
	}

	return err
}

func (t *Task) SaveQuitTimeToDB() {
	t.EndTime = time.Now()
	otime := time.Since(t.ScartTime)
	over_time := util.FmtDuration(otime)
	t.Dm.SaveQuitTime(t.TaskId, t.EndTime, over_time)
}

func (t *Task) agentPluginRun(args *proxy.PassiveProxy) {

	AllowDomains, err := t.TaskConfig.GetValue("allow_domain")
	if err != nil {
		panic(err)
	}

	ForbiddenDomains, err := t.TaskConfig.GetValue("forbit_domain")
	if err != nil {
		panic(err)
	}

	scan_depth, err := t.TaskConfig.GetValue("scan_depth")
	if err != nil {
		panic(err)
	}

	sdint, err := strconv.Atoi(scan_depth.String())
	if err != nil {
		panic(err)
	}
	go func() {
		SocketHandler()
	}()

	if args != nil {
		go func() {
			for {
				UrlElement := <-args.CommunicationSingleton

				if value, ok := UrlElement["IsPauseScan"].(bool); ok {
					plugin.IsPauseScan = value
				} else if urlinfo, ok := UrlElement["agent"].([]interface{}); ok {
					var iscontinue = false
					for _, e := range urlinfo {
						//处理允许与不允许的域名
						em := e.(map[string]interface{})
						//AllowDomains
						ADs := strings.Split(AllowDomains.String(), "|")
						for _, v := range ADs {
							if !funk.Contains(em["url"], v) {
								iscontinue = true
								continue
							}
						}
						//ForbiddenDomain
						fds := strings.Split(ForbiddenDomains.String(), "|")
						for _, v := range fds {
							if funk.Contains(em["url"], v) {
								iscontinue = true
								continue
							}
						}

						//处理扫描深度，超过的跳出
						if util.GetScanDeepByUrl(em["url"].(string)) >= sdint {
							iscontinue = true
							continue
						}
					}
					if iscontinue {
						continue
					}
					t.TaskConfig.JsonOrYaml = false

					t.EnablePluginsALLURL(UrlElement, args.HttpsCert, args.HttpsCertKey, true, config.UnconfirmSocket)
				}

			}
		}()
	}

}

// removetasks 移除总任务进度的任务ID
func removetasks(id int) {
	for index, t := range Tasks {
		if t.TaskId == id {
			Tasks = append(Tasks[:index], Tasks[index+1:]...)
		}
	}
}

func (t *Task) Init() {
	Ctx, Cancel := context.WithCancel(context.Background())
	t.Ctx = &Ctx
	t.Cancel = &Cancel
	t.lock = &sync.Mutex{}
	t.PliuginsMsg = make(chan map[string]interface{}, 1)
	t.stoppluginmsg = make(chan struct{})
	t.DoStartSignal = make(chan bool, 1)
	t.ScartTime = time.Now()
	global.VulnResultReporter.Exweb_task_info.Start_time = t.ScartTime.Local().Format("2006-01-02 15:04:05")
}

func (t *Task) UrlExpand(_url string, extras ...interface{}) error {
	var (
		err      error
		Domainid int64
	)
	Headers := make(map[string]interface{})

	for _, extra := range extras {
		if id, ok := extra.(int64); ok {
			Domainid = id
		}
		if Header, ok := extra.(map[string]interface{}); ok {
			Headers = Header
		}
	}

	_url = util.RepairUrl(_url)

	url, err := model.GetUrl(_url)
	if err != nil {
		logger.Error(err.Error())
		return err
	}

	Headers["HOST"] = url.Path

	t.Targets = append(t.Targets, &model.Request{
		URL:           url,
		Method:        "GET",
		FasthttpProxy: t.TaskConfig.Yaml.Proxy,
		Headers:       Headers,
		DomainId:      Domainid,
	})
	return err
}

func CmdHandler(c *cli.Context, t *Task, tconfig tconfig) {
	logger.Info("Enter command mode...")

	err := config.ReadYamlTaskConf(ConfigpPath, &t.TaskConfig.Yaml)
	jsonconf, _ := config.ReadJsonConfig("itop_task.json")
	if err != nil {
		logger.Error("test ReadTaskConf() fail")
	}
	for _, _url := range c.Args().Slice() {
		t.UrlExpand(_url, nil)
	}
	t.TaskConfig.Json = jsonconf
	if strings.ToLower(Configtype) == "json" {
		t.TaskConfig.JsonOrYaml = false
	} else {
		t.TaskConfig.JsonOrYaml = true
	}

	t.XssSpider.Init(t.TaskConfig)
	//t.PluginBrowser = &t.XssSpider

	// config.ProxyPort = 1966
	t.dostartTasks(tconfig)
	t.PluginWg.Wait()
}

func WebSocketHandler() error {
	l, err := net.Listen("tcp", WebSocket)
	if err != nil {
		logger.Error("%s", err.Error())
		return err
	}
	logger.Info("WebSocket listening on ws://%v", l.Addr())

	cs, err := NewTaskServer("websocket")
	if err != nil {
		logger.Error(err.Error())
		return err
	}

	s := &http.Server{
		Handler: cs,
	}

	errc := make(chan error, 1)
	go func() {
		errc <- s.Serve(l)
	}()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)

	select {
	case err := <-errc:
		logger.Error("failed to serve: %v", err)
	case sig := <-sigs:
		logger.Error("terminating: %v", sig)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	return s.Shutdown(ctx)
}

func SocketHandler() error {
	var m MConn
	errc := make(chan error, 1)
	m.Init()
	server_control, err := NewTaskServer("socket")
	m.CallbackFunc = server_control.Task
	if err != nil {
		logger.Error(err.Error())
		return err
	}

	listener, err := net.Listen("tcp", Socket)
	if err != nil {
		logger.Error(err.Error())
		return err
	}
	defer listener.Close()
	go func() {
		for {
			con, err := listener.Accept()
			if err != nil {
				logger.Error(err.Error())
				continue
			}
			go m.Listen(con)
			netcomm.SOCKETCONN = append(netcomm.SOCKETCONN, &con)
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)

	select {
	case err := <-errc:
		logger.Error("failed to serve: %v", err)
	case sig := <-sigs:
		logger.Error("terminating: %v", sig)
	}

	return err
}