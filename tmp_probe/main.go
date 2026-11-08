package main

// Reproduces the reader's body measurement for a stored mail body: the same
// srcdoc shape and the same measuring code as web/src/reader, reporting what
// each side sees, so a clipped last line shows who under-measures.

import (
	"encoding/base64"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

var bodyHTML []byte

const frameCSS = `*{overscroll-behavior:none}html,body{margin:0;padding:0;background:transparent}
html{-webkit-text-size-adjust:100%}
#godwit-body{overflow-x:auto;overflow-y:hidden;overflow-wrap:break-word;text-autospace:normal}
#godwit-body table{max-width:100%}
#godwit-body img{max-width:100% !important}
::where(#godwit-body img:not([height]:not([width]))){height:auto}
::where([data-href]){cursor:pointer;text-decoration:underline;color:#0b57d0}`

const pageTmpl = `<!doctype html><html><head><meta charset="utf-8">
<style>body{margin:0}iframe{border:0;width:100%;display:block}</style></head>
<body><iframe id="f" sandbox="allow-same-origin allow-scripts"></iframe>
<script>
const beacon=(s)=>{ new Image().src='http://app.probe.localhost/log/'+encodeURIComponent(s); };
window.onerror=(e)=>beacon('ERR '+e);
beacon('script start');
const FRAME = '<!doctype html><html><head>'
 + '<meta http-equiv="Content-Security-Policy" content="default-src \'none\'; style-src \'unsafe-inline\'; img-src data: godwit-img: http://*.godwit-img.localhost">'
 + '<meta charset="utf-8"><style>__CSS__</style></head>'
 + '<body><div id="godwit-body" class="godwit-light">' + atob('__BODY64__') + '</div></body></html>';
const f = document.getElementById('f');
f.srcdoc = FRAME;
function state(){
  try{
    const d=f.contentDocument, w=f.contentWindow;
    const wrap=d.getElementById('godwit-body');
    const last=wrap.lastElementChild;
    const r=wrap.getBoundingClientRect(), lr=last.getBoundingClientRect();
    const cs=w.getComputedStyle(last);
    return JSON.stringify({
      wrap: r.height, last_bottom: lr.bottom, last_tag: last.tagName,
      last_mb: cs.marginBottom, de_scroll: d.documentElement.scrollHeight,
      body_scroll: d.body.scrollHeight, iframe: f.getBoundingClientRect().height
    });
  }catch(e){ return 'err:'+e }
}
let n=0;
const iv=setInterval(()=>{
  n++;
  const s='RESULT#'+n+' '+state();
  document.title=s;
  beacon(s);
  if(n>8) clearInterval(iv);
},400);
</script></body></html>`

func page() string {
	css := strings.ReplaceAll(frameCSS, "\n", "") +
		`html{font:14px/normal -apple-system,BlinkMacSystemFont,"Helvetica Neue",sans-serif}`
	s := strings.ReplaceAll(pageTmpl, "__CSS__", css)
	return strings.ReplaceAll(s, "__BODY64__", base64.StdEncoding.EncodeToString(bodyHTML))
}

func main() {
	var err error
	bodyHTML, err = os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	if err := mygo.Protocol.HandleFunc("probe", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("HIT %s", r.URL.Path)
		if strings.HasPrefix(r.URL.Path, "/app/log/") {
			t, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/app/log/"))
			log.Printf("%s", t)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(page()))
	}); err != nil {
		log.Fatal(err)
	}
	mygo.App.SetName("imgproto")
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{Title: "probe", Width: 800, Height: 900})
		win.Show()
		win.Page().LoadURL("probe://localhost/")
		go func() {
			last := ""
			for range 40 {
				time.Sleep(300 * time.Millisecond)
				if t := win.Title(); strings.Contains(t, "RESULT") && t != last {
					last = t
					log.Printf("TITLE %s", t)
				}
			}
			time.Sleep(2 * time.Second)
			mygo.App.Quit()
		}()
	})
	mygo.App.Run()
}
