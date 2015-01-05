package Work

import (
	"errors"
	"fmt"
	"github.com/ihsw/go-download/Blizzard/Auction"
	"github.com/ihsw/go-download/Blizzard/AuctionData"
	"github.com/ihsw/go-download/Cache"
	"github.com/ihsw/go-download/Entity"
	"github.com/ihsw/go-download/Entity/Character"
	"github.com/ihsw/go-download/Util"
	"time"
)

/*
	error structs
*/
func RunQueue(regionRealms map[int64][]Entity.Realm, downloadIn chan Entity.Realm, itemizeOut chan ItemizeResult, totalRealms int, cacheClient Cache.Client) (err error) {
	// formatting the realms to be evenly distributed
	largestRegion := 0
	for _, realms := range regionRealms {
		if len(realms) > largestRegion {
			largestRegion = len(realms)
		}
	}
	formattedRealms := make([]map[int64]Entity.Realm, largestRegion)
	for regionId, realms := range regionRealms {
		for i, realm := range realms {
			if formattedRealms[int64(i)] == nil {
				formattedRealms[int64(i)] = map[int64]Entity.Realm{}
			}
			formattedRealms[int64(i)][regionId] = realm
		}
	}

	// populating the download queue
	for _, realms := range formattedRealms {
		for _, realm := range realms {
			downloadIn <- realm
		}
	}

	// waiting for the results to drain out
	results := make([]ItemizeResult, totalRealms)
	totalValidResults := 0
	for i := 0; i < totalRealms; i++ {
		result := <-itemizeOut
		if result.err != nil {
			err = errors.New(fmt.Sprintf("itemizeOut %s (%d) had an error (%s)", result.realm.Dump(), result.realm.Id, result.err.Error()))
			return
		}
		if result.alreadyChecked {
			err = errors.New(fmt.Sprintf("Realm %s (%d) has already been checked", result.realm.Dump(), result.realm.Id))
			return
		}

		results[i] = result

		if !result.responseFailed {
			totalValidResults++
		}
	}

	// gathering valid results
	validResults := make([]ItemizeResult, totalValidResults)
	i := 0
	for _, result := range results {
		if !result.responseFailed {
			validResults[i] = result
			i++
		}
	}
	fmt.Println(fmt.Sprintf("Total results: %d", len(results)))
	fmt.Println(fmt.Sprintf("Valid results: %d", len(validResults)))
	// itemizeResults := ItemizeResults{list: validResults}

	// gathering items from the results
	// newItems := itemizeResults.GetUniqueItems()

	// persisting them
	// itemManager := Entity.ItemManager{Client: cacheClient}
	// _, err = itemManager.PersistAll(newItems)
	// if err != nil {
	// 	return
	// }

	return nil
}

func DownloadRealm(realm Entity.Realm, cacheClient Cache.Client, out chan DownloadResult) {
	// misc
	var (
		auctionResponse     *Auction.Response
		auctionDataResponse *AuctionData.Response
		err                 error
	)
	realmManager := Entity.RealmManager{Client: cacheClient}
	result := DownloadResult{Result: Result{realm: realm}}

	// fetching the auction info
	auctionResponse, err = Auction.Get(realm, cacheClient.ApiKey)
	if err != nil {
		result.err = errors.New(fmt.Sprintf("Auction.Get() failed (%s)", err.Error()))
		out <- result
		return
	}

	// optionally halting on empty response
	if auctionResponse == nil {
		result.responseFailed = true
		out <- result
		return
	}

	file := auctionResponse.Files[0]

	// checking whether the file has already been downloaded
	lastModified := time.Unix(file.LastModified/1000, 0)
	if !realm.LastDownloaded.IsZero() && (realm.LastDownloaded.Equal(lastModified) || realm.LastDownloaded.Before(lastModified)) {
		result.alreadyChecked = true
		out <- result
		return
	}
	fmt.Println(fmt.Sprintf("Found last modified for %s: %s", realm.Dump(), lastModified.Format(Util.WriteLayout)))

	// fetching the actual auction data
	auctionDataResponse, err = AuctionData.Get(realm, file.Url)
	if err != nil {
		result.err = errors.New(fmt.Sprintf("AuctionData.Get() failed (%s)", err.Error()))
		out <- result
		return
	}

	// optionally halting on empty response
	if auctionDataResponse == nil {
		result.responseFailed = true
		out <- result
		return
	}

	// loading it into the response
	result.auctionDataResponse = auctionDataResponse

	// flagging the realm as having been downloaded
	realm.LastDownloaded = lastModified
	realmManager.Persist(realm)

	// queueing it out
	out <- result
}

func ItemizeRealm(downloadResult DownloadResult, cacheClient Cache.Client, out chan ItemizeResult) {
	// misc
	var err error
	realm := downloadResult.realm
	result := ItemizeResult{Result: downloadResult.Result}
	out <- result
	return

	// optionally halting on error
	if downloadResult.err != nil {
		result.err = errors.New(fmt.Sprintf("downloadResult had an error (%s)", downloadResult.err.Error()))
		out <- result
		return
	}

	// optionally halting for whatever reason
	if !downloadResult.CanContinue() {
		out <- result
		return
	}

	/*
		character handling
	*/
	characterManager := Character.Manager{Client: cacheClient}

	// gathering existing characters
	var existingCharacters []Character.Character
	existingCharacters, err = characterManager.FindByRealm(realm)
	if err != nil {
		result.err = errors.New(fmt.Sprintf("CharacterManager.FindByRealm() failed (%s)", err.Error()))
		out <- result
		return
	}

	// merging existing characters in and persisting them all
	result.characters, err = characterManager.PersistAll(realm, existingCharacters, downloadResult.getNewCharacters(existingCharacters))
	if err != nil {
		result.err = errors.New(fmt.Sprintf("CharacterManager.PersistAll() failed (%s)", err.Error()))
		out <- result
		return
	}

	/*
		item handling
	*/
	result.blizzItemIds = downloadResult.getBlizzItemIds()

	/*
		auction handling
	*/
	// gathering auctions for post-itemize processing
	result.auctions = downloadResult.auctionDataResponse.Auctions.Auctions

	// queueing it out
	out <- result
}
