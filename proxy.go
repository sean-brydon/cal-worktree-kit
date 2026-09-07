package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Route struct {
	Host             string `json:"host"`
	Port             int    `json:"port"`
	Active           bool   `json:"active"`
	StudioPort       int    `json:"studio_port"`
	Shared           bool   `json:"shared"`
	TailnetURL       string `json:"tailnet_url"`
	TailnetStudioURL string `json:"tailnet_studio_url"`
	ShareBusy        bool   `json:"share_busy"`
	ShareError       string `json:"share_error"`
}

func main() {
	listen := flag.String("listen", "127.0.0.1:18080", "loopback listen address")
	routes := flag.String("routes", "", "directory of route records")
	laptop := flag.Bool("laptop", false, "route work and personal namespaces to Tailmux")
	flag.Parse()
	if !*laptop {
		startTLSRelays(*routes)
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ResponseHeaderTimeout: 5 * time.Minute}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(r.Host)
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.TrimSuffix(host, ".")
		port := 0
		sharedRequest := false
		cookieKey := ""
		tailnet, _ := r.Context().Value(tailnetIngressKey{}).(bool)
		if *laptop {
			if strings.HasSuffix(host, ".work.cal.localhost") {
				port = 18080
			}
			if strings.HasSuffix(host, ".personal.cal.localhost") {
				port = 18081
			}
		} else {
			files, _ := filepath.Glob(filepath.Join(*routes, "*.json"))
			for _, file := range files {
				b, err := os.ReadFile(file)
				if err != nil {
					continue
				}
				var route Route
				if json.Unmarshal(b, &route) != nil || route.Port < 1024 || route.Port > 65535 {
					continue
				}
				appURL, _ := url.Parse(route.TailnetURL)
				studioURL, _ := url.Parse(route.TailnetStudioURL)
				sharedApp := tailnet && route.Shared && route.Active && appURL != nil && strings.EqualFold(r.Host, appURL.Host)
				sharedStudio := tailnet && route.Shared && route.Active && studioURL != nil && strings.EqualFold(r.Host, studioURL.Host)
				localMatch := !tailnet && (host == route.Host || strings.HasSuffix(host, "."+route.Host))
				if localMatch || sharedApp || sharedStudio {
					sharedRequest = sharedApp || sharedStudio
					cookieKey = strings.TrimSuffix(filepath.Base(file), ".json")
					if (host == route.Host || sharedApp) && strings.HasPrefix(r.URL.Path, "/__worktree/share") {
						serveShare(w, r, file, route, !tailnet)
						return
					}
					if localMatch && route.Shared && route.Active && !strings.HasPrefix(r.URL.Path, "/__worktree/") {
						destination := route.TailnetURL
						if host == "studio."+route.Host {
							destination = route.TailnetStudioURL
						}
						w.Header().Set("Cache-Control", "no-store")
						http.Redirect(w, r, destination+r.URL.RequestURI(), http.StatusTemporaryRedirect)
						return
					}
					if (host == route.Host || sharedApp) && (r.URL.Path == "/__worktree/logs" || r.URL.Path == "/__worktree/logs/data") {
						serveLogs(w, r, file, route.Active)
						return
					}
					if route.Active {
						if (host == route.Host || sharedApp) && (r.URL.Path == "/__worktree/studio" || r.URL.Path == "/__worktree/studio/") {
							destination := "http://studio." + route.Host + "/"
							if route.Shared {
								destination = route.TailnetStudioURL + "/"
							}
							http.Redirect(w, r, destination, http.StatusTemporaryRedirect)
							return
						}
						if host == "studio."+route.Host || sharedStudio {
							port = route.StudioPort
							available := false
							if port >= 6100 && port < 7100 {
								conn, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
								if e == nil {
									available = true
									conn.Close()
								}
							}
							if !available {
								key := strings.TrimSuffix(filepath.Base(file), ".json")
								if !keyPattern.MatchString(key) {
									http.Error(w, "Invalid worktree", 404)
									return
								}
								ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
								command := exec.CommandContext(ctx, filepath.Join(filepath.Dir(filepath.Dir(file)), "cal-worktree"), "studio", key)
								output, e := command.Output()
								cancel()
								if e != nil {
									http.Error(w, "Studio could not start. Inspect the host's cal-studio service log.", 503)
									return
								}
								port, e = strconv.Atoi(strings.TrimSpace(string(output)))
								if e != nil || port < 6100 || port >= 7100 {
									http.Error(w, "Invalid Studio port", 503)
									return
								}
							}
						} else {
							port = route.Port
						}
					}
					break
				}
			}
		}
		if port == 0 {
			http.Error(w, "No active Cal.com worktree for this URL. Run its Orca setup hook to start it.", 404)
			return
		}
		if sharedRequest && !validDevOrigin(r) {
			http.Error(w, "Cross-origin development assets are not allowed", 403)
			return
		}
		target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
		proxy := &httputil.ReverseProxy{Transport: transport, FlushInterval: -1, Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = r.Host
			pr.SetXForwarded()
			pr.Out.Header.Set("X-Forwarded-Host", r.Host)
			pr.Out.Header.Set("X-Forwarded-Proto", "http")
			if sharedRequest {
				pr.Out.Header.Set("X-Forwarded-Proto", "https")
				normalizeDevOrigin(pr.Out)
				isolateRequestCookies(pr.Out, cookieKey)
			}
		}, ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("upstream %s %s: %v", r.Host, r.URL.Path, err)
			http.Error(w, "This worktree is starting or its Tailmux connection is unavailable. Retry shortly.", 503)
		}}
		if sharedRequest {
			proxy.ModifyResponse = func(response *http.Response) error {
				isolateResponseCookies(response, cookieKey)
				if response.Header.Get("Access-Control-Allow-Origin") == "http://localhost" {
					response.Header.Set("Access-Control-Allow-Origin", "https://"+r.Host)
				}
				return nil
			}
		}
		proxy.ServeHTTP(w, r)
	})
	if !*laptop {
		go func() {
			server := &http.Server{Addr: "127.0.0.1:18082", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handler.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tailnetIngressKey{}, true)))
			}), ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 90 * time.Second}
			log.Fatal(server.ListenAndServe())
		}()
	}
	s := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 90 * time.Second}
	log.Printf("Cal worktree proxy listening on %s", *listen)
	log.Fatal(s.ListenAndServe())
}

var keyPattern = regexp.MustCompile(`^[a-f0-9]{12}$`)

func serveLogs(w http.ResponseWriter, r *http.Request, file string, active bool) {
	key := strings.TrimSuffix(filepath.Base(file), ".json")
	if !keyPattern.MatchString(key) {
		http.Error(w, "Invalid worktree", 404)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'")
	if r.Method != "GET" {
		http.Error(w, "GET only", 405)
		return
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "Open logs directly from the worktree URL", 403)
		return
	}
	if r.URL.Path == "/__worktree/logs" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, logsPage)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "journalctl", "--user", "--unit=cal-worktree-"+key+".service", "--lines=200", "--no-pager", "--output=short-iso", "--quiet", "--all")
	pipe, err := cmd.StdoutPipe()
	var runtime []byte
	if err == nil {
		err = cmd.Start()
	}
	if err == nil {
		runtime, err = io.ReadAll(io.LimitReader(pipe, 128*1024))
		if len(runtime) == 128*1024 {
			cancel()
		}
		waitErr := cmd.Wait()
		if err == nil {
			err = waitErr
		}
	}
	if err != nil && len(runtime) == 0 {
		runtime = []byte("Runtime logs unavailable: " + err.Error())
	}
	var setup []byte
	f, err := os.Open(filepath.Join(filepath.Dir(filepath.Dir(file)), key+"-setup.log"))
	if err == nil {
		defer f.Close()
		if stat, e := f.Stat(); e == nil && stat.Size() > 128*1024 {
			f.Seek(-128*1024, io.SeekEnd)
		}
		setup, _ = io.ReadAll(io.LimitReader(f, 128*1024))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"active": active, "runtime": string(runtime), "setup": string(setup)})
}

const logsPage = `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Worktree logs</title>
<style>body{margin:0;background:#101316;color:#dce5eb;font:14px system-ui}header{position:sticky;top:0;background:#171c21;padding:20px 24px;border-bottom:1px solid #33404b;display:flex;gap:16px;align-items:center;flex-wrap:wrap}h1{font-size:18px;margin:0}a{color:#8bd5ba}button,select{background:#252f38;color:inherit;border:1px solid #465360;padding:8px 12px;border-radius:6px}#state{color:#a9b7c3}pre{padding:12px 24px;white-space:pre-wrap;overflow-wrap:anywhere;font:12px/1.65 ui-monospace,monospace}small{padding:0 24px;color:#8e9dab}</style>
<header><h1>Worktree logs</h1><span id="state">Connecting…</span><select id="source" aria-label="Log source"><option value="runtime">Running app</option><option value="setup">Setup</option></select><button id="pause">Pause</button><label><input type="checkbox" id="follow" checked> Follow</label><a href="/">Open app ↗</a><a href="/__worktree/share">Share ↗</a></header><pre id="output" aria-live="off"></pre><small>Updates every 2 seconds · latest 200 runtime entries · bounded setup history</small>
<script>
const palette=['#15191e','#ff7b83','#83d69b','#e8ca79','#85b9ff','#d4a0f5','#79d8dc','#dce5eb','#8b99a8','#ffa0a5','#a7edb7','#ffe49a','#aacfff','#e9baff','#a4f0ee','#ffffff'];
function renderAnsi(target,text){
 target.replaceChildren();let style={};
 const colour=n=>n<16?palette[n]:n<232?'rgb('+[Math.floor((n-16)/36),Math.floor((n-16)/6)%6,(n-16)%6].map(x=>x?55+x*40:0).join(',')+')':'rgb('+Array(3).fill(8+(n-232)*10).join(',')+')';
 // Discard terminal-only controls, including hyperlinks; log text never becomes HTML.
 text=text.replace(/\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g,'').replace(/\x1b\[[0-?]*[ -/]*[A-HJKSTfhl]/g,'');
 for(const line of text.split('\n')){
  const fallback=/\b(error|failed|failure|fatal)\b|\b[45]\d\d\b/i.test(line)?palette[1]:/\b(warn|warning)\b|YN0002|YN0086/i.test(line)?palette[3]:/\b(ready|success|successful|completed)\b|✓|\b20[0-9]\b/i.test(line)?palette[2]:/\b(info|starting|compiling)\b/i.test(line)?palette[6]:'';
  const chunks=line.split(/(\x1b\[[0-9;]*m)/g);
  for(const chunk of chunks){
   if(chunk.startsWith('\x1b[')){const codes=(chunk.slice(2,-1)||'0').split(';').map(Number);for(let i=0;i<codes.length;i++){let c=codes[i];if(c===0)style={};else if(c===1)style.fontWeight='bold';else if(c===2)style.opacity='.7';else if(c===3)style.fontStyle='italic';else if(c===4)style.textDecoration='underline';else if(c===22){delete style.fontWeight;delete style.opacity}else if(c===23)delete style.fontStyle;else if(c===24)delete style.textDecoration;else if(c===39)delete style.color;else if(c===49)delete style.backgroundColor;else if(c>=30&&c<=37)style.color=palette[c-30];else if(c>=90&&c<=97)style.color=palette[c-90+8];else if(c>=40&&c<=47)style.backgroundColor=palette[c-40];else if(c>=100&&c<=107)style.backgroundColor=palette[c-100+8];else if(c===38||c===48){const prop=c===38?'color':'backgroundColor';if(codes[i+1]===5&&codes[i+2]<=255){style[prop]=colour(codes[i+2]);i+=2}else if(codes[i+1]===2&&codes.slice(i+2,i+5).length===3&&codes.slice(i+2,i+5).every(n=>n>=0&&n<=255)){style[prop]='rgb('+codes.slice(i+2,i+5).join(',')+')';i+=4}}}continue}
   const span=document.createElement('span');span.textContent=chunk;Object.assign(span.style,{color:line.includes('\x1b[')?'':fallback},style);target.append(span);
  }target.append(document.createTextNode('\n'));
 }
}
let paused=false,data={};const el=id=>document.getElementById(id);function render(){renderAnsi(el('output'),data[el('source').value]||'No output yet.');if(el('follow').checked)window.scrollTo(0,document.body.scrollHeight)}el('source').onchange=render;el('pause').onclick=()=>{paused=!paused;el('pause').textContent=paused?'Resume':'Pause'};async function poll(){if(!paused&&!document.hidden){try{const r=await fetch('/__worktree/logs/data',{cache:'no-store'});if(!r.ok)throw Error('HTTP '+r.status);data=await r.json();el('state').textContent=(data.active?'Active worktree':'Archived / stopped')+' · '+new Date().toLocaleTimeString();render()}catch(e){el('state').textContent='Disconnected · '+e.message}}setTimeout(poll,2000)}poll()</script></html>`

// A separate loopback ingress prevents tailnet callers spoofing a local Host to
// operate sharing controls. The normal ingress is only reached over private SSH.
type tailnetIngressKey struct{}

func authCookieName(name string) bool {
	return strings.HasPrefix(strings.TrimPrefix(strings.TrimPrefix(name, "__Secure-"), "__Host-"), "next-auth.")
}
func cookieNamespace(name, key string) string {
	for _, prefix := range []string{"__Secure-", "__Host-"} {
		if strings.HasPrefix(name, prefix) {
			return prefix + "wt-" + key + "-" + strings.TrimPrefix(name, prefix)
		}
	}
	return "wt-" + key + "-" + name
}
func isolateRequestCookies(r *http.Request, key string) {
	cookies := r.Cookies()
	r.Header.Del("Cookie")
	for _, cookie := range cookies {
		name := cookie.Name
		prefix := ""
		for _, candidate := range []string{"__Secure-", "__Host-"} {
			if strings.HasPrefix(name, candidate) {
				prefix = candidate
				name = strings.TrimPrefix(name, candidate)
				break
			}
		}
		if strings.HasPrefix(name, "wt-") {
			own := "wt-" + key + "-"
			if !strings.HasPrefix(name, own) {
				continue
			}
			cookie.Name = prefix + strings.TrimPrefix(name, own)
		} else if authCookieName(cookie.Name) {
			continue
		}
		r.AddCookie(cookie)
	}
}
func isolateResponseCookies(r *http.Response, key string) {
	cookies := r.Cookies()
	r.Header.Del("Set-Cookie")
	for _, cookie := range cookies {
		if authCookieName(cookie.Name) {
			cookie.Name = cookieNamespace(cookie.Name, key)
			cookie.Domain = ""
		}
		r.Header.Add("Set-Cookie", cookie.String())
	}
}

func serveShare(w http.ResponseWriter, r *http.Request, file string, route Route, local bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	key := strings.TrimSuffix(filepath.Base(file), ".json")
	if !keyPattern.MatchString(key) {
		http.Error(w, "Invalid worktree", 404)
		return
	}
	if r.URL.Path == "/__worktree/share" && r.Method == "GET" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, sharePage)
		return
	}
	if r.URL.Path != "/__worktree/share/data" {
		http.NotFound(w, r)
		return
	}
	if r.Method == "GET" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"shared": route.Shared, "busy": route.ShareBusy, "error": route.ShareError, "active": route.Active, "canManage": local, "url": route.TailnetURL, "studio": route.TailnetStudioURL, "local": "http://" + route.Host})
		return
	}
	if r.Method != "POST" {
		http.Error(w, "GET or POST only", 405)
		return
	}
	// Exact Origin is required even for simple form submissions and same-site subdomains.
	if !local || r.Header.Get("Origin") != "http://"+route.Host || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "Use the local worktree sharing page", 403)
		return
	}
	if !route.Active {
		http.Error(w, "Start the worktree first", 409)
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body) != nil || (body.Action != "on" && body.Action != "off") {
		http.Error(w, "Choose on or off", 400)
		return
	}
	// Finish a requested transition even if its browser tab closes.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, filepath.Join(filepath.Dir(filepath.Dir(file)), "cal-worktree"), "share", key, body.Action)
	output, err := command.CombinedOutput()
	if err != nil {
		log.Printf("share %s: %v %s", key, err, output)
		http.Error(w, "Sharing could not change. Refresh for status; inspect the proxy service log.", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"ok":true}`)
}

// TLS stays encrypted all the way from the Mac through the existing Tailmux SSH
// connection to Tailscale Serve. These loopback relays do not terminate HTTPS.
func startTLSRelays(routes string) {
	for slot := 0; slot < 20; slot++ {
		for _, port := range []int{18443 + slot, 19443 + slot} {
			listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				log.Printf("TLS relay %d: %v", port, err)
				continue
			}
			go func(port int, listener net.Listener) {
				for {
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					go relayTLS(conn, routes, port)
				}
			}(port, listener)
		}
	}
}
func relayTLS(conn net.Conn, routes string, port int) {
	defer conn.Close()
	var config struct {
		IP string `json:"ip"`
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(routes), "sharing.json"))
	if err != nil || json.Unmarshal(data, &config) != nil {
		return
	}
	ip := net.ParseIP(config.IP)
	_, tailrange, _ := net.ParseCIDR("100.64.0.0/10")
	if ip == nil || !tailrange.Contains(ip) {
		return
	}
	upstream, err := net.DialTimeout("tcp", net.JoinHostPort(config.IP, strconv.Itoa(port)), 10*time.Second)
	if err != nil {
		return
	}
	defer upstream.Close()
	done := make(chan struct{})
	go func() {
		io.Copy(upstream, conn)
		if tcp, ok := upstream.(*net.TCPConn); ok {
			tcp.CloseWrite()
		}
		close(done)
	}()
	io.Copy(conn, upstream)
	conn.Close()
	<-done
}

const sharePage = `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Share worktree</title>
<style>body{margin:48px auto;padding:0 24px;max-width:740px;background:#101316;color:#dce5eb;font:16px/1.6 system-ui}h1{font-size:26px}a{color:#8bd5ba;overflow-wrap:anywhere}button{padding:10px 16px;border:1px solid #465360;border-radius:8px;background:#25352f;color:#dce5eb;font:inherit;cursor:pointer}button:disabled{opacity:.5;cursor:wait}section{margin:24px 0;padding:20px;border:1px solid #33404b;border-radius:12px}#state{font-weight:600}#error{color:#ff9d9d}small{color:#adbbc4}.row{margin:12px 0}input{box-sizing:border-box;width:100%;background:#171c21;color:#dce5eb;border:1px solid #465360;border-radius:6px;padding:12px;font:14px monospace}</style>
<h1>Share this worktree</h1><p>Let colleagues on the work Tailscale network use this running branch. The app, logs and Prisma Studio are shared, including database editing.</p>
<section><p id="state">Loading…</p><p id="error"></p><button id="toggle" disabled>Share with tailnet</button><p><small>One app and one branch database. Changing sharing briefly restarts the app. Local app shortcuts redirect to HTTPS while sharing is on. This does not publish to the internet.</small></p></section>
<section id="links" hidden><label>Tailnet URL<input id="url" readonly aria-label="Tailnet URL"></label><div class="row"><button id="copy">Copy link</button> <span id="copied"></span></div><div class="row"><a id="app">Open shared app ↗</a></div><div class="row"><a id="logs">Live logs ↗</a></div><div class="row"><a id="studio">Prisma Studio ↗</a></div></section><p><a id="local">Local sharing controls ↗</a></p><p><small>Colleagues need Tailscale access to this VPS. Third-party OAuth providers need the shared callback URL registered separately. Sessions and ongoing requests may need a refresh when the address changes.</small></p>
<script>let state={},changing=false;const el=id=>document.getElementById(id);async function refresh(){try{const r=await fetch('/__worktree/share/data',{cache:'no-store'});if(!r.ok)throw Error('HTTP '+r.status);state=await r.json();el('state').textContent=state.busy?'Changing sharing…':!state.active?'Worktree stopped — sharing is paused':state.shared?'Shared with work tailnet':'Only available locally through Tailmux';el('error').textContent=state.error||'';el('toggle').textContent=state.shared?'Stop sharing':'Share with tailnet';el('toggle').disabled=changing||state.busy||!state.active||!state.canManage;el('links').hidden=!state.shared||!state.active;el('url').value=state.url||'';el('app').href=state.url||'#';el('logs').href=(state.url||'')+'/__worktree/logs';el('studio').href=(state.url||'')+'/__worktree/studio';el('local').href=state.local+'/__worktree/share'}catch(e){el('error').textContent=e.message}}el('toggle').onclick=async()=>{changing=true;el('toggle').disabled=true;el('state').textContent='Changing sharing and restarting app…';try{const r=await fetch('/__worktree/share/data',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:state.shared?'off':'on'})});if(!r.ok)throw Error(await r.text())}catch(e){el('error').textContent=e.message}finally{changing=false;await refresh()}};el('copy').onclick=async()=>{el('url').select();try{await navigator.clipboard.writeText(el('url').value);el('copied').textContent='Copied'}catch{el('copied').textContent=document.execCommand('copy')?'Copied':'Press ⌘C / Ctrl+C to copy'}};refresh();setInterval(()=>{if(!changing)refresh()},3000)</script></html>`

// Next checks dev assets against its bind hostname. Preserve that check at this
// proxy boundary, then translate only a validated same-origin dev-resource request.
func isDevResource(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/_next/") || strings.HasPrefix(r.URL.Path, "/__nextjs")
}
func validDevOrigin(r *http.Request) bool {
	if !isDevResource(r) {
		return true
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "https://"+r.Host {
		return false
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	return true
}
func normalizeDevOrigin(r *http.Request) {
	if !isDevResource(r) {
		return
	}
	if r.Header.Get("Origin") != "" {
		r.Header.Set("Origin", "http://localhost")
	}
}
