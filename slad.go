package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/tatsushid/go-fastping"
)

type ByDuration []time.Duration

func (t ByDuration) Len() int {
	return len(t)
}
func (t ByDuration) Swap(i, j int) {
	t[i], t[j] = t[j], t[i]
}
func (t ByDuration) Less(i, j int) bool {
	return t[i] < t[j]
}

type Latency struct {
	IP      []string
	STATUS  string
	RTT     []time.Duration
	LATENCY time.Duration
}

type LatencyClient struct {
	Host string
}

func IPLookup(url string) (arr []string) {

	result := []string{}
	ra, err := net.LookupIP(url)

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	for _, ip := range ra {
		result = append(result, ip.String())
	}
	return result
}
func ping(url string, result map[string][]time.Duration, done chan bool) {

	ra, err := net.LookupIP(url)

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	p := fastping.NewPinger()

	for _, ip := range ra {
		ipAddr, _ := net.ResolveIPAddr("ip4:icmp", ip.String())
		result[ip.String()] = []time.Duration{}
		p.AddIPAddr(ipAddr)
	}

	p.OnRecv = func(addr *net.IPAddr, rtt time.Duration) {
		fmt.Printf("IP Addr: %s receive, RTT: %v\n", addr.String(), rtt)
		result[addr.String()] = append(result[addr.String()], rtt)
	}
	p.MaxRTT = 300 * time.Millisecond
	p.RunLoop()

	if err != nil {
		fmt.Println(err)
	}

	select {
	case <-p.Done():
		if err := p.Err(); err != nil {
			log.Fatalf("Ping failed: %v", err)
		}
	case <-time.After(5 * time.Minute):
		break
	}
	p.Stop()

	done <- true
}

func (tc *LatencyClient) PingLatency(url string) (Latency, error) {
	var respLatency Latency

	bidRequest := []byte(`
			{	"imp": [
			{
				"banner": {
					"w": 300,
					"h": 250,
					"pos": 1,
					"topframe": 1
				},
				"secure": 0,
				"id": "1"
			}
			],
			"site": {
				"cat": [
				"IAB10"
				],
				"page": "http://discipline.about.com/od/decreasenegativebehaviors/fl/7-Steps-to-Creating-a-Behavior-Chart-for-Your-Child.htm?utm_term=kids%20discipline%20chart&utm_content=p1-main-1-title&utm_medium=sem&utm_source=gemini&utm_campaign=adid-1357adc1-cba5-4713-b2bc-fb1106406e17-0-ab_tse_ocode-33060&ad=semD&an=gemini_s&am=exact&q=kids%20discipline%20chart&dqi=&o=33060&l=sem&qsrc=999&askid=1357adc1-cba5-4713-b2bc-fb1106406e17-0-ab_tse",
				"ref": "http://index.about.com/index?am=exact&q=kids+discipline+chart&an=gemini_s&askid=1357adc1-cba5-4713-b2bc-fb1106406e17-0-ab_tse&dqi=&qsrc=999&ad=semD&o=33060&l=sem",
				"content": {
					"keywords": "1"
				},
				"id": "166825"
			},
			"id": "CF98DCC0C388C337",
			"ext": {
				"ssl": 0,
				"sdepth": 1,
				"edepth": 1
			},
			"device": {
				"ua": "Mozilla/5.0 (iPad; CPU OS 8_4_1 like Mac OS X) AppleWebKit/600.1.4 (KHTML, like Gecko) Version/8.0 Mobile/12H321 Safari/600.1.4",
				"language": "EN",
				"ip": "66.56.51.232"
			}
		}`)
	t0 := time.Now()
	r, err := makeRequest("POST", url, bidRequest)
	t1 := time.Now()
	dt := t1.Sub(t0)
	respLatency.LATENCY = dt

	err = processResponse(r, 200)
	respLatency.STATUS = "ok"

	return respLatency, err
}

func makeRequest(method string, url string, body []byte) (*http.Response, error) {

	buffer := bytes.NewBuffer(body)

	req, err := http.NewRequest(method, url, buffer)

	if err != nil {
		return nil, err
	}

	return http.DefaultClient.Do(req)
}

func processResponse(r *http.Response, expectedStatus int) error {
	if r.StatusCode != expectedStatus {
		log.Fatal("response status of " + r.Status)
	}
	return nil
}

func Routine(url string, ip string, port string, tripC chan []time.Duration) {
	tripTime := []time.Duration{}
	lc := &LatencyClient{url}

	for i := 0; i < 1000; i++ {

		latencyResponse, err := lc.PingLatency("http://" + ip + port)

		if err != nil {
			log.Fatal(err)
		} else if latencyResponse.STATUS == "ok" {
			tripTime = append(tripTime, latencyResponse.LATENCY)
		}
		time.Sleep(300 * time.Millisecond)
	}

	sort.Sort(ByDuration(tripTime))
	tripC <- tripTime
}

func main() {

	URL := flag.String("URL", "delivery.ny.bloom.datacratic.com", "URL string")
	port := flag.String("Port", ":9951", " string")

	flag.Parse()

	timeLimit := 5 * time.Millisecond
	done := make(chan bool)
	resultPing := map[string][]time.Duration{}

	go ping(*URL, resultPing, done)

	IPs := IPLookup(*URL)
	resultLatency := map[string]chan []time.Duration{}

	for _, ip := range IPs {
		resultLatency[ip] = make(chan []time.Duration)
		go Routine(*URL, ip, *port, resultLatency[ip])
	}

	<-done

	latencyTrip99 := map[string]time.Duration{}
	for ip, res := range resultLatency {
		r := <-res
		percentile := float64(99) / float64(100) * float64(len(r))
		latencyTrip99[ip] = r[int(percentile)]
	}

	pingTrip99 := map[string]time.Duration{}
	for adds, rt := range resultPing {
		sort.Sort(ByDuration(rt))
		percentile := float64(99) / float64(100) * float64(len(rt))
		pingTrip99[adds] = rt[int(percentile)]
	}

	percentile75 := []time.Duration{}
	for addres, t1 := range pingTrip99 {
		if t2, ok := latencyTrip99[addres]; ok {
			timeLatency := t2 - t1
			percentile75 = append(percentile75, timeLatency)
			fmt.Println("address", "latency", addres, timeLatency)
		}
	}
	sort.Sort(ByDuration(percentile75))
	percentile := float64(75) / float64(100) * float64(len(percentile75))
	AppLatency := percentile75[int(percentile)]

	if AppLatency > timeLimit {
		fmt.Println("Downtime Due To Exccesive Latency")
	}
}
