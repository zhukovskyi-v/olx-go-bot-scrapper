package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/fentezi/olx-scraper/internal"
	"github.com/fentezi/olx-scraper/logger"
	"github.com/fentezi/olx-scraper/models"
	"github.com/fentezi/olx-scraper/storage"
	"github.com/fentezi/olx-scraper/utils"
	"github.com/joho/godotenv"
	"golang.org/x/time/rate"
	tele "gopkg.in/telebot.v3"
)

const (
	maxPagesPerPoll    = 5
	notifyFailureCount = 5
	pauseFailureCount  = 50
	maxRateLimitWait   = 10 * time.Minute
	maxBackoff         = 5 * time.Minute
)

var (
	stateMu     sync.Mutex
	urlState    = make(map[int64]bool)
	cancelFuncs = make(map[int64]map[string]context.CancelFunc)
	store       *storage.Store
	log         *slog.Logger

	globalLimiter  = rate.NewLimiter(rate.Limit(25), 30)
	chatLimitersMu sync.Mutex
	chatLimiters   = make(map[int64]*rate.Limiter)
)

func chatLimiter(id int64) *rate.Limiter {
	chatLimitersMu.Lock()
	defer chatLimitersMu.Unlock()
	if l, ok := chatLimiters[id]; ok {
		return l
	}
	l := rate.NewLimiter(rate.Limit(1), 3)
	chatLimiters[id] = l
	return l
}

func main() {
	_ = godotenv.Load()
	log = logger.Logger()
	log.Info("application started")

	token := os.Getenv("TOKEN")
	if token == "" {
		log.Error("missing token")
		os.Exit(1)
	}

	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		log.Error("missing DB_URL (libsql://...?authToken=...)")
		os.Exit(1)
	}
	s, err := storage.Open(dbURL)
	if err != nil {
		log.Error("failed to open store", logger.Err(err))
		os.Exit(1)
	}
	store = s
	defer store.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pref := tele.Settings{
		Token:   token,
		Poller:  &tele.LongPoller{Timeout: 10 * time.Second},
		Verbose: false,
	}
	b, err := tele.NewBot(pref)
	if err != nil {
		log.Error("failed to init bot", logger.Err(err))
		os.Exit(1)
	}

	TelegramInit(ctx, b)

	watches, err := store.AllWatches()
	if err != nil {
		log.Error("failed to load watches", logger.Err(err))
	} else {
		for uid, urls := range watches {
			for _, u := range urls {
				spawnWatch(ctx, b, uid, u)
			}
		}
	}

	go purgeLoop(ctx)

	go b.Start()
	log.Info("bot started", "goroutines", runtime.NumGoroutine())

	<-ctx.Done()
	log.Info("shutting down")
	b.Stop()
}

func purgeLoop(ctx context.Context) {
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := store.PurgeOldSeen(30 * 24 * time.Hour); err != nil {
				log.Warn("purge failed", "err", err.Error())
			}
		}
	}
}

func spawnWatch(parentCtx context.Context, b *tele.Bot, userID int64, listURL string) {
	stateMu.Lock()
	if cancelFuncs[userID] == nil {
		cancelFuncs[userID] = make(map[string]context.CancelFunc)
	}
	if existing, ok := cancelFuncs[userID][listURL]; ok {
		existing()
	}
	watchCtx, watchCancel := context.WithCancel(parentCtx)
	cancelFuncs[userID][listURL] = watchCancel
	stateMu.Unlock()
	go parseLoop(watchCtx, userID, listURL, b)
}

func cancelWatch(userID int64, listURL string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	inner := cancelFuncs[userID]
	if inner == nil {
		return
	}
	if c, ok := inner[listURL]; ok {
		c()
		delete(inner, listURL)
	}
	if len(inner) == 0 {
		delete(cancelFuncs, userID)
	}
}

func stopAllWatches(userID int64) []string {
	stateMu.Lock()
	defer stateMu.Unlock()
	urls := make([]string, 0, len(cancelFuncs[userID]))
	for u, c := range cancelFuncs[userID] {
		c()
		urls = append(urls, u)
	}
	delete(cancelFuncs, userID)
	return urls
}

func hasRunningWatch(userID int64, listURL string) bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	if inner, ok := cancelFuncs[userID]; ok {
		_, running := inner[listURL]
		return running
	}
	return false
}

func parseLoop(ctx context.Context, id int64, listURL string, b *tele.Bot) {
	log.Info("parseLoop started", "id", id, "url", listURL)
	defer log.Info("parseLoop exited", "id", id, "url", listURL)

	consecutiveFailures := 0
	for {
		bootstrapped, err := store.IsBootstrapped(id, listURL)
		if err != nil {
			log.Warn("isBootstrapped failed", "err", err.Error())
		}

		pollErr := pollOnce(ctx, id, listURL, b, bootstrapped)
		if pollErr != nil {
			consecutiveFailures++
			log.Warn("poll failed", "id", id, "attempt", consecutiveFailures, "err", pollErr.Error())

			if consecutiveFailures == notifyFailureCount {
				notify(ctx, b, id, "Парсинг временно приостановлен — OLX недоступен. Повторим автоматически.")
			}
			if consecutiveFailures >= pauseFailureCount {
				if err := store.SetPaused(id, listURL, true); err != nil {
					log.Warn("setPaused failed", "err", err.Error())
				}
				notify(ctx, b, id, "Парсинг приостановлен после долгих сбоев OLX. Используйте /resume для возобновления.")
				cancelWatch(id, listURL)
				return
			}

			var rl *utils.RateLimitError
			var wait time.Duration
			if errors.As(pollErr, &rl) {
				wait = rl.RetryAfter
				if wait > maxRateLimitWait {
					wait = maxRateLimitWait
				}
			} else {
				wait = time.Duration(math.Min(maxBackoff.Seconds(), 5*math.Pow(2, float64(consecutiveFailures-1)))) * time.Second
			}
			if !sleepCtx(ctx, wait) {
				return
			}
			continue
		}

		consecutiveFailures = 0
		if !bootstrapped {
			if err := store.SetBootstrapped(id, listURL); err != nil {
				log.Warn("setBootstrapped failed", "err", err.Error())
			}
		}

		sleepDuration := time.Duration(30+rand.Intn(90)) * time.Second
		if !sleepCtx(ctx, sleepDuration) {
			return
		}
	}
}

func pollOnce(ctx context.Context, id int64, listURL string, b *tele.Bot, bootstrapped bool) error {
	maxPages := maxPagesPerPoll
	if !bootstrapped {
		maxPages = 1
	}

	for page := 1; page <= maxPages; page++ {
		pageURL, err := paginatedURL(listURL, page)
		if err != nil {
			return err
		}

		doc, err := utils.FetchAndParseHTML(pageURL)
		if err != nil {
			return err
		}

		ads, err := internal.ParseList(doc)
		if err != nil {
			return err
		}

		sawSeen := false
		for _, ad := range ads {
			if ad.ID == "" {
				continue
			}
			seen, err := store.HasSeen(id, ad.ID)
			if err != nil {
				log.Warn("hasSeen failed", "err", err.Error())
				continue
			}
			if seen {
				sawSeen = true
				continue
			}

			if !bootstrapped {
				if err := store.MarkSeen(id, ad.ID); err != nil {
					log.Warn("markSeen failed", "err", err.Error())
				}
				continue
			}

			detail, derr := fetchDetail(ad.URL)
			if derr != nil {
				log.Warn("detail fetch failed, sending list info", "id", id, "url", ad.URL, "err", derr.Error())
			} else {
				if detail.Description != "" {
					ad.Description = detail.Description
				}
				if detail.Price != "" {
					ad.Price = detail.Price
				}
				if detail.District != "" {
					ad.District = detail.District
				}
				if detail.Image != "" {
					ad.Image = detail.Image
				}
				if detail.Title != "" {
					ad.Title = detail.Title
				}
			}

			if err := store.MarkSeen(id, ad.ID); err != nil {
				log.Warn("markSeen failed", "err", err.Error())
			}

			if err := SendMessageAd(ctx, b, id, ad); err != nil {
				log.Warn("send failed", "id", id, "err", err.Error())
			} else {
				log.Info("sent ad", "id", id, "adID", ad.ID, "title", ad.Title)
			}
		}

		if sawSeen {
			break
		}
	}
	return nil
}

func paginatedURL(raw string, page int) (string, error) {
	if page <= 1 {
		return raw, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("page", strconv.Itoa(page))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func notify(ctx context.Context, b *tele.Bot, id int64, msg string) {
	if err := globalLimiter.Wait(ctx); err != nil {
		return
	}
	if err := chatLimiter(id).Wait(ctx); err != nil {
		return
	}
	if _, err := b.Send(tele.ChatID(id), msg); err != nil {
		log.Warn("notify failed", "id", id, "err", err.Error())
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func fetchDetail(adURL string) (models.Ad, error) {
	doc, err := utils.FetchAndParseHTML(adURL)
	if err != nil {
		return models.Ad{}, err
	}
	return internal.ParseDetail(doc)
}

func SendMessageAd(ctx context.Context, b *tele.Bot, id int64, ad models.Ad) error {
	caption := ad.Title
	if ad.Price != "" {
		caption += "\nЦена: " + ad.Price
	}
	location := ad.City
	if ad.District != "" {
		if location != "" {
			location += ", " + ad.District
		} else {
			location = ad.District
		}
	}
	if location != "" {
		caption += "\nГород: " + location
	}

	btnURL := tele.InlineButton{
		Unique: "myButton",
		Text:   "Объявление",
		URL:    ad.URL,
	}
	markup := &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{btnURL}}}

	send := func(payload interface{}) error {
		if err := globalLimiter.Wait(ctx); err != nil {
			return err
		}
		if err := chatLimiter(id).Wait(ctx); err != nil {
			return err
		}
		_, err := b.Send(tele.ChatID(id), payload, &tele.SendOptions{ReplyMarkup: markup})
		return err
	}

	sendWithRetry := func(payload interface{}) error {
		err := send(payload)
		if err == nil {
			return nil
		}
		var fe tele.FloodError
		if errors.As(err, &fe) {
			wait := time.Duration(fe.RetryAfter) * time.Second
			if wait <= 0 {
				wait = 5 * time.Second
			}
			if !sleepCtx(ctx, wait) {
				return ctx.Err()
			}
			return send(payload)
		}
		return err
	}

	if ad.Image != "" {
		photo := &tele.Photo{File: tele.FromURL(ad.Image), Caption: caption}
		if err := sendWithRetry(photo); err == nil {
			return nil
		}
	}
	return sendWithRetry(caption)
}

func TelegramInit(ctx context.Context, b *tele.Bot) {
	b.Handle("/start", func(c tele.Context) error {
		return c.Send(fmt.Sprintf("Привет, %s! Я бот для парсинга объявлений на OLX. Чтобы начать, нажмите команду /addurl", c.Sender().Username))
	})

	b.Handle("/addurl", func(c tele.Context) error {
		userID := c.Sender().ID
		stateMu.Lock()
		urlState[userID] = true
		stateMu.Unlock()
		message := "Введите URL-адрес для парсинга." +
			"\n\nНа OLX выберите нужный город, категорию товаров, фильтры и параметры поиска. " +
			"После того, как все критерии заданы, скопируйте URL-адрес из адресной строки браузера и отправьте его боту." +
			"\n\nПример подходящей ссылки: https://www.olx.ua/uk/nedvizhimost/kvartiry/prodazha-kvartir/"
		return c.Send(message)
	})

	b.Handle(tele.OnText, func(c tele.Context) error {
		userID := c.Sender().ID
		stateMu.Lock()
		awaiting := urlState[userID]
		stateMu.Unlock()

		if !awaiting {
			return c.Send(c.Text())
		}

		text := c.Text()
		if !isValidURL(text) {
			return c.Send("Неверный URL. Пожалуйста, попробуйте еще раз.")
		}

		if err := store.AddWatch(userID, text); err != nil {
			log.Error("addWatch failed", logger.Err(err))
			return c.Send("Не удалось сохранить URL. Попробуйте позже.")
		}

		stateMu.Lock()
		urlState[userID] = false
		stateMu.Unlock()

		spawnWatch(ctx, b, userID, text)
		return c.Send("URL успешно добавлен.\n\nКак только подходящие объявления появятся, бот оповестит вас.")
	})

	b.Handle("/stopparse", func(c tele.Context) error {
		userID := c.Sender().ID
		urls := stopAllWatches(userID)
		if len(urls) == 0 {
			return c.Send("Парсинг еще не начат!")
		}
		for _, u := range urls {
			if err := store.RemoveWatch(userID, u); err != nil {
				log.Warn("removeWatch failed", "err", err.Error())
			}
		}
		return c.Send("Парсер остановлен!")
	})

	b.Handle("/resume", func(c tele.Context) error {
		userID := c.Sender().ID
		urls, err := store.ResumeWatch(userID)
		if err != nil {
			log.Error("resume failed", logger.Err(err))
			return c.Send("Не удалось возобновить парсинг.")
		}
		if len(urls) == 0 {
			return c.Send("Нет сохранённых URL для возобновления.")
		}
		for _, u := range urls {
			if !hasRunningWatch(userID, u) {
				spawnWatch(ctx, b, userID, u)
			}
		}
		return c.Send("Парсинг возобновлён.")
	})

	b.Handle("/help", func(c tele.Context) error {
		helpMessage := "Доступные команды:\n/addurl - добавить ссылку на категорию или поиск OLX для отслеживания новых объявлений. Бот будет парсить указанную страницу и оповещать о свежих публикациях\n/stopparse - остановить парсинг\n/resume - возобновить парсинг после паузы\n/help - помощь"
		return c.Send(helpMessage)
	})
}

var urlPattern = regexp.MustCompile(`^(https?://)?(www\.)?olx\.(ua|pl|bg|ro|pt|com|co\.za|com\.br|com\.pk|lt|lv|hr|kz|uz|by|md|az)/.*$`)

func isValidURL(str string) bool {
	if !urlPattern.MatchString(str) {
		return false
	}
	_, err := url.ParseRequestURI(str)
	return err == nil
}
