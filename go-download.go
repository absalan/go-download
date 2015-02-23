package main

import (
	"flag"
	"fmt"
	"github.com/ihsw/go-download/Blizzard/Status"
	"github.com/ihsw/go-download/Cache"
	"github.com/ihsw/go-download/Entity"
	"github.com/ihsw/go-download/Misc"
	"github.com/ihsw/go-download/Util"
	"runtime"
	"sync"
	"time"
)

type StatusGetResult struct {
	region   Entity.Region
	response Status.Response
	err      error
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	flushDb := flag.Bool("flush", false, "Clears all redis dbs")
	configPath := flag.String("config", "", "Config path")
	// isProd := flag.Bool("prod", false, "Prod mode")
	flag.Parse()

	output := Util.Output{StartTime: time.Now()}
	output.Write("Starting...")

	// init
	var (
		client  Cache.Client
		regions []Entity.Region
		err     error
	)
	if client, regions, err = Misc.Init(*configPath, *flushDb); err != nil {
		output.Write(fmt.Sprintf("Misc.Init() fail: %s", err.Error()))
		return
	}

	// misc
	statusGetIn := make(chan Entity.Region)
	statusGetOut := make(chan StatusGetResult)
	wg := new(sync.WaitGroup)
	const statusWorkerCount = 4

	// spawning some workers
	wg.Add(statusWorkerCount)
	for i := 0; i < statusWorkerCount; i++ {
		go func() {
			for region := range statusGetIn {
				response, err := Status.Get(region, client.ApiKey)
				statusGetOut <- StatusGetResult{
					region:   region,
					err:      err,
					response: response,
				}
			}
			wg.Done()
		}()
	}

	// queueing up the in channel
	go func() {
		for _, region := range regions {
			statusGetIn <- region
		}
		close(statusGetIn)
	}()

	// waiting for results to drain out
	go func() {
		wg.Wait()
		close(statusGetOut)
	}()

	// gathering the results
	for result := range statusGetOut {
		if err = result.err; err != nil {
			output.Write(fmt.Sprintf("StatusGet() had an error: %s", err.Error()))
			return
		}

		realmManger := Entity.NewRealmManager(client)
		for _, responseRealm := range result.response.Realms {
			realm := Entity.Realm{
				Name:        responseRealm.Name,
				Slug:        responseRealm.Slug,
				Battlegroup: responseRealm.Battlegroup,
				Type:        responseRealm.Type,
				Status:      responseRealm.Status,
				Population:  responseRealm.Population,
			}
			if realm, err = realmManger.Persist(realm); err != nil {
				output.Write(fmt.Sprintf("RealmManager.Persist() fail: %s", err.Error()))
				return
			}
		}
	}

	output.Conclude()
}
