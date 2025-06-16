package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- 定数とスタイル ---
const (
	mainDir      = "GoMusicDownloader"
	downloadsDir = "downloads"
	tempDir      = "temp"
	logsDir      = "logs"
	cmdTimeout   = 30 * time.Second
)

var (
	// Colors (Dracula-like theme)
	fgColor       = lipgloss.Color("#f8f8f2")
	commentColor  = lipgloss.Color("#6272a4")
	cyanColor     = lipgloss.Color("#8be9fd")
	greenColor    = lipgloss.Color("#50fa7b")
	pinkColor     = lipgloss.Color("#ff79c6")
	purpleColor   = lipgloss.Color("#bd93f9")
	redColor      = lipgloss.Color("#ff5555")

	appStyle = lipgloss.NewStyle().Margin(1, 2)

	headerStyle = lipgloss.NewStyle().
			Foreground(fgColor).
			Background(purpleColor).
			Padding(0, 1).
			Bold(true)

	helpStyle = lipgloss.NewStyle().Foreground(commentColor)

	// List Styles
	listTitleStyle = lipgloss.NewStyle().
			Background(pinkColor).
			Foreground(fgColor).
			Padding(0, 1)

	paginationStyle = list.DefaultStyles().PaginationStyle.PaddingLeft(4)
)

// --- モデルと状態 ---
type model struct {
	state         state
	input         textinput.Model
	tagInputs     []textinput.Model
	focusIndex    int
	spinner       spinner.Model
	ytResults     list.Model
	mbResults     list.Model
	tracklist     list.Model
	selectedYT    item
	selectedMB    item
	selectedTrack item
	statusMsg     string
	error         error
	ytDlpPath     string
	ffmpegPath    string
	width         int
	height        int
	lastFile      string
}

type state int

const (
	stateCheckingDeps state = iota
	stateInput
	stateFetchingURLInfo
	stateSearching
	stateSelectYT
	stateSelectMB
	stateSelectTrack
	stateEditTags
	stateDownloading
	stateShowSuccess
	stateConfirmSkipMB
	stateError
)

type item struct {
	title, desc, id, url, artist, itemType string
	meta                                 interface{}
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title + " " + i.desc }

type finalTags struct {
	Title, Artist, Album, Date, TrackNumber, AlbumArtist, Lyrics string
	DurationSec                                                int
}

// --- メッセージ ---
type (
	ytDlpCheckResultMsg  struct{ path string; err error }
	ffmpegCheckResultMsg struct{ path string; err error }
	urlInfoFetchedMsg    struct{ ytItem item; err error }
	searchFinishedMsg    struct{ ytItems, mbItems []list.Item; err error }
	mbSearchFinishedMsg  struct{ items []list.Item; err error }
	tracklistFinishedMsg struct{ items []list.Item; err error }
	downloadFinishedMsg  struct{ filename string; err error }
	resetMsg             struct{}
)

// --- JSON構造体 ---
type ytDlpVideoInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Uploader string `json:"uploader"`
	Channel  string `json:"channel"`
}

type (
	MusicBrainzSearchResponse struct{ Releases []MBRelease `json:"releases"` }
	MBRelease struct {
		ID           string         `json:"id"`
		Title        string         `json:"title"`
		ArtistCredit []MBArtist     `json:"artist-credit"`
		Date         string         `json:"date"`
		Media        []MBMedia      `json:"media"`
		ReleaseGroup MBReleaseGroup `json:"release-group"`
	}
	MBReleaseGroup struct{ ID, PrimaryType string `json:"id", "primary-type"` }
	MBArtist struct {
		Name       string `json:"name"`
		JoinPhrase string `json:"joinphrase"`
	}
	MBMedia struct {
		Format string    `json:"format"`
		Tracks []MBTrack `json:"tracks"`
	}
	MBTrack struct {
		ID        string      `json:"id"`
		Title     string      `json:"title"`
		Number    string      `json:"number"`
		Length    int         `json:"length"` // in milliseconds
		Recording MBRecording `json:"recording"`
	}
	MBRecording struct{ Genres []MBGenre `json:"genres"` }
	MBGenre     struct{ Name string `json:"name"` }
)

type LrclibResponse struct {
	PlainLyrics string `json:"syncedLyrics"`
}

// --- Custom Delegate for List ---
type itemDelegate struct{}

func (d itemDelegate) Height() int                             { return 2 }
func (d itemDelegate) Spacing() int                            { return 1 }
func (d itemDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(item)
	if !ok {
		return
	}
	selectedTitleStyle := lipgloss.NewStyle().PaddingLeft(2).Foreground(cyanColor).Bold(true)
	selectedDescStyle := lipgloss.NewStyle().PaddingLeft(2).Foreground(purpleColor)
	normalTitleStyle := lipgloss.NewStyle().PaddingLeft(2).Foreground(fgColor)
	normalDescStyle := lipgloss.NewStyle().PaddingLeft(2).Foreground(commentColor)

	if index == m.Index() {
		title := selectedTitleStyle.Render("▶ " + i.title)
		desc := selectedDescStyle.Render("  " + i.desc)
		fmt.Fprint(w, lipgloss.JoinVertical(lipgloss.Left, title, desc))
	} else {
		title := normalTitleStyle.Render("  " + i.title)
		desc := normalDescStyle.Render("  " + i.desc)
		fmt.Fprint(w, lipgloss.JoinVertical(lipgloss.Left, title, desc))
	}
}

func newModel() model {
	ti := textinput.New()
	ti.Placeholder = "アーティスト名と曲名、またはYouTubeのURLを入力してください..."
	ti.Focus()
	ti.Width = 60
	s := spinner.New()
	s.Spinner = spinner.Pulse
	s.Style = lipgloss.NewStyle().Foreground(pinkColor)
	return model{
		state:     stateCheckingDeps,
		statusMsg: "依存関係を確認中...",
		input:     ti,
		spinner:   s,
		ytResults: newList("", nil),
		mbResults: newList("", nil),
		tracklist: newList("", nil),
	}
}

// --- Bubble Tea ---
func (m model) Init() tea.Cmd { return checkYtDlpCmd }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		listHeight := m.height - 8
		listWidth := m.width - 4
		m.ytResults.SetSize(listWidth, listHeight)
		m.mbResults.SetSize(listWidth, listHeight)
		m.tracklist.SetSize(listWidth, listHeight)

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		switch m.state {
		case stateSelectYT:
			if msg.Type == tea.KeyEnter {
				if i, ok := m.ytResults.SelectedItem().(item); ok {
					m.selectedYT = i
					m.state = stateSearching
					m.statusMsg = "MusicBrainzでメタデータを検索中です..."
					cmds = append(cmds, m.spinner.Tick, searchMusicBrainzCmd(fmt.Sprintf("%s %s", i.title, i.desc)))
				}
			} else if msg.Type == tea.KeyEsc {
				m.state = stateInput
			}
		case stateSelectMB:
			if msg.Type == tea.KeyEnter {
				if i, ok := m.mbResults.SelectedItem().(item); ok {
					m.selectedMB = i
					m.state = stateSelectTrack
					m.statusMsg = "トラックリストを取得中です..."
					cmds = append(cmds, m.spinner.Tick, getTracklistCmd(i.id))
				}
			} else if msg.String() == "s" {
				m.state = stateConfirmSkipMB
			} else if msg.Type == tea.KeyEsc {
				m.state = stateSelectYT
			}
		case stateSelectTrack:
			if msg.Type == tea.KeyEnter {
				if i, ok := m.tracklist.SelectedItem().(item); ok {
					m.selectedTrack = i
					m.state = stateEditTags
					m.focusIndex = 0
					m.tagInputs = m.createTagInputs()
					cmds = append(cmds, m.tagInputs[0].Focus())
				}
			} else if msg.Type == tea.KeyEsc {
				m.state = stateSelectMB
			}
		case stateEditTags:
			if msg.Type == tea.KeyEnter {
				if m.focusIndex == len(m.tagInputs)-1 {
					m.state, m.statusMsg = stateDownloading, "音声・ジャケット・歌詞を取得中です..."
					trackInfo := m.selectedTrack.meta.(MBTrack)
					tags := finalTags{
						Title:       m.tagInputs[0].Value(),
						Artist:      m.tagInputs[1].Value(),
						Album:       m.tagInputs[2].Value(),
						Date:        m.tagInputs[3].Value(),
						TrackNumber: m.tagInputs[4].Value(),
						AlbumArtist: m.tagInputs[1].Value(),
						DurationSec: trackInfo.Length / 1000,
					}
					cmds = append(cmds, m.spinner.Tick, downloadCmd(m.ytDlpPath, m.ffmpegPath, m.selectedYT, m.selectedMB, tags))
				} else {
					m.focusIndex++
					cmds = append(cmds, m.tagInputs[m.focusIndex].Focus())
				}
			} else if msg.Type == tea.KeyEsc {
				m.state = stateSelectTrack
			} else {
				if msg.String() == "up" {
					m.focusIndex--
				} else if msg.String() == "down" {
					m.focusIndex++
				}
				if m.focusIndex < 0 {
					m.focusIndex = len(m.tagInputs) - 1
				} else if m.focusIndex >= len(m.tagInputs) {
					m.focusIndex = 0
				}
				for i := range m.tagInputs {
					if i == m.focusIndex {
						cmds = append(cmds, m.tagInputs[i].Focus())
					} else {
						m.tagInputs[i].Blur()
					}
				}
			}
		case stateInput:
			if msg.Type == tea.KeyEnter {
				query := m.input.Value()
				if strings.HasPrefix(query, "http") {
					m.state, m.statusMsg = stateFetchingURLInfo, "URLから情報を取得中です..."
					cmds = append(cmds, m.spinner.Tick, getURLInfoCmd(m.ytDlpPath, query))
				} else {
					m.state, m.statusMsg = stateSearching, "YouTubeとMusicBrainzを検索中です..."
					cmds = append(cmds, m.spinner.Tick, searchCmd(m.ytDlpPath, query))
				}
			}
		case stateConfirmSkipMB:
			switch strings.ToLower(msg.String()) {
			case "y", "enter":
				m.state, m.statusMsg = stateDownloading, "タグ無しでダウンロード中です..."
				cmds = append(cmds, m.spinner.Tick, simpleDownloadCmd(m.ytDlpPath, m.ffmpegPath, m.selectedYT))
			case "n", "esc":
				m.state = stateSelectYT
			}
		case stateShowSuccess, stateError:
			cmds = append(cmds, func() tea.Msg { return resetMsg{} })
		}

	// --- Async Messages ---
	case ytDlpCheckResultMsg:
		if msg.err != nil {
			m.state, m.error = stateError, msg.err
		} else {
			m.ytDlpPath = msg.path
			cmds = append(cmds, checkFfmpegCmd)
		}
	case ffmpegCheckResultMsg:
		if msg.err != nil {
			m.state, m.error = stateError, fmt.Errorf("ffmpegが見つかりません。\n音声変換には必須です。OSに合わせてインストールしてください。\n(例: brew install ffmpeg)")
		} else {
			m.ffmpegPath, m.state = msg.path, stateInput
		}
	case urlInfoFetchedMsg:
		if msg.err != nil {
			m.state, m.error = stateError, msg.err
		} else {
			m.selectedYT = msg.ytItem
			m.state, m.statusMsg = stateSearching, "MusicBrainzでメタデータを検索中です..."
			cmds = append(cmds, m.spinner.Tick, searchMusicBrainzCmd(fmt.Sprintf("%s %s", msg.ytItem.title, msg.ytItem.desc)))
		}
	case searchFinishedMsg:
		if msg.err != nil {
			m.state, m.error = stateError, msg.err
		} else {
			m.state = stateSelectYT
			m.ytResults = newList("どの音源をダウンロードしますか？", msg.ytItems)
			m.mbResults = newList("どのリリースからタグ情報を取得しますか？", msg.mbItems)
			m.ytResults.SetSize(m.width-4, m.height-8)
		}
	case mbSearchFinishedMsg:
		if msg.err != nil {
			m.state, m.error = stateError, msg.err
		} else if len(msg.items) == 0 {
			m.state = stateConfirmSkipMB
		} else {
			m.state = stateSelectMB
			m.mbResults = newList("どのリリースからタグ情報を取得しますか？", msg.items)
			m.mbResults.SetSize(m.width-4, m.height-8)
		}
	case tracklistFinishedMsg:
		if msg.err != nil {
			m.state, m.error = stateError, msg.err
		} else if len(msg.items) == 0 {
			m.state, m.error = stateError, fmt.Errorf("選択したリリースにはトラック情報が含まれていませんでした。別のリリースを選択してください。")
		} else {
			m.state = stateSelectTrack
			m.tracklist = newList(fmt.Sprintf("「%s」から曲を選択してください", m.selectedMB.title), msg.items)
			m.tracklist.SetSize(m.width-4, m.height-8)
		}
	case downloadFinishedMsg:
		if msg.err != nil {
			m.state, m.error = stateError, msg.err
		} else {
			m.state, m.lastFile = stateShowSuccess, msg.filename
		}
	case resetMsg:
		ytPath, ffPath, w, h := m.ytDlpPath, m.ffmpegPath, m.width, m.height
		m = newModel()
		m.ytDlpPath, m.ffmpegPath, m.width, m.height = ytPath, ffPath, w, h
		m.state = stateInput
		m.statusMsg = ""
		cmds = append(cmds, textinput.Blink)
	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	switch m.state {
	case stateInput:
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	case stateSelectYT:
		m.ytResults, cmd = m.ytResults.Update(msg)
		cmds = append(cmds, cmd)
	case stateSelectMB:
		m.mbResults, cmd = m.mbResults.Update(msg)
		cmds = append(cmds, cmd)
	case stateSelectTrack:
		m.tracklist, cmd = m.tracklist.Update(msg)
		cmds = append(cmds, cmd)
	case stateEditTags:
		if m.focusIndex < len(m.tagInputs) {
			m.tagInputs[m.focusIndex], cmd = m.tagInputs[m.focusIndex].Update(msg)
			cmds = append(cmds, cmd)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	var finalView string

	if m.state == stateShowSuccess {
		successBox := lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(greenColor).Padding(1, 2).Align(lipgloss.Center).Render(fmt.Sprintf("%s\n%s", lipgloss.NewStyle().Foreground(greenColor).Render("✅ ダウンロード完了"), m.lastFile))
		help := helpStyle.Render("何かキーを押すと最初の画面に戻ります...")
		finalView = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Center, successBox, help))
	} else {
		var content, help string
		switch m.state {
		case stateCheckingDeps, stateFetchingURLInfo, stateSearching, stateDownloading:
			content = fmt.Sprintf("\n %s %s\n", m.spinner.View(), m.statusMsg)
			help = helpStyle.Render("  Ctrl+C: 終了")
		case stateInput:
			content = fmt.Sprintf("\n%s\n", m.input.View())
			help = helpStyle.Render("  Enter: 検索 | Ctrl+C: 終了")
		case stateConfirmSkipMB:
			content = fmt.Sprintf("\n%s\n\n%s", "MusicBrainzにデータが見つかりませんでした。", "YouTubeのタイトルを元にタグ無しでダウンロードしますか？")
			help = helpStyle.Render("  y/Enter: はい | n/Esc: いいえ")
		case stateSelectYT, stateSelectMB, stateSelectTrack:
			lists := map[state]list.Model{stateSelectYT: m.ytResults, stateSelectMB: m.mbResults, stateSelectTrack: m.tracklist}
			content = lists[m.state].View()
			if m.state == stateSelectMB {
				help = helpStyle.Render("  Enter: 決定 | ↑/↓: 移動 | s: スキップ | Esc: 戻る | Ctrl+C: 終了")
			} else {
				help = helpStyle.Render("  Enter: 決定 | ↑/↓: 移動 | Esc: 戻る | Ctrl+C: 終了")
			}
		case stateEditTags:
			var b strings.Builder
			b.WriteString("\nメタデータを確認・編集してください:\n\n")
			labels := []string{"タイトル:", "アーティスト:", "アルバム:", "リリース日:", "トラック番号:"}
			for i, input := range m.tagInputs {
				b.WriteString(fmt.Sprintf("  %s %s\n", labels[i], input.View()))
			}
			content = b.String()
			help = helpStyle.Render("  ↑/↓: 移動 | Enter: 次へ/決定 | Esc: 戻る | Ctrl+C: 終了")
		case stateError:
			errorBox := lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(redColor).Padding(1, 2).Render(fmt.Sprintf("%s\n%s", lipgloss.NewStyle().Foreground(redColor).Render("❌ エラーが発生しました"), m.error.Error()))
			content = lipgloss.Place(m.width-4, m.height-7, lipgloss.Center, lipgloss.Center, errorBox)
			help = helpStyle.Render("  何かキーを押すと最初の画面に戻ります...")
		}
		header := headerStyle.Render("🎵 yt-Music Downloader v1.0 by andromeda")
		mainContent := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(purpleColor).Width(m.width - 4).Height(m.height - 7).Render(content)
		finalView = appStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, mainContent, help))
	}
	return finalView
}

func (m *model) createTagInputs() []textinput.Model {
	inputs := make([]textinput.Model, 5)
	releaseInfo := m.selectedMB.meta.(MBRelease)
	trackInfo := m.selectedTrack.meta.(MBTrack)
	values := []string{trackInfo.Title, m.selectedTrack.artist, releaseInfo.Title, releaseInfo.Date, trackInfo.Number}
	for i := range inputs {
		inputs[i] = textinput.New()
		inputs[i].SetValue(values[i])
		inputs[i].Width = 50
		inputs[i].CharLimit = 150
	}
	return inputs
}

// --- Commands and Helpers ---
func newList(title string, items []list.Item) list.Model {
	l := list.New(items, itemDelegate{}, 0, 0)
	l.Title = title
	l.Styles.Title = listTitleStyle
	l.Styles.PaginationStyle = list.DefaultStyles().PaginationStyle.PaddingLeft(4)
	l.SetShowHelp(false)
	return l
}

func joinArtistCredits(credits []MBArtist) string {
	var b strings.Builder
	for _, credit := range credits {
		b.WriteString(credit.Name)
		b.WriteString(credit.JoinPhrase)
	}
	return b.String()
}

func checkYtDlpCmd() tea.Msg {
	path, err := exec.LookPath("yt-dlp")
	if err == nil {
		return ytDlpCheckResultMsg{path: path}
	}
	localPath := "yt-dlp"
	if runtime.GOOS == "windows" {
		localPath += ".exe"
	}
	if _, err := os.Stat(localPath); err == nil {
		return ytDlpCheckResultMsg{path: "./" + localPath}
	}
	return ytDlpCheckResultMsg{err: fmt.Errorf("yt-dlpが見つかりません。パスが通っているか、実行ファイルと同じフォルダに配置してください。")}
}
func checkFfmpegCmd() tea.Msg {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ffmpegCheckResultMsg{err: err}
	}
	return ffmpegCheckResultMsg{path: path}
}
func getURLInfoCmd(ytDlpPath, query string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, ytDlpPath, "--quiet", "--no-warnings", "--dump-json", query)
		output, err := cmd.CombinedOutput()
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return urlInfoFetchedMsg{err: fmt.Errorf("URL情報の取得がタイムアウトしました (30s)")}
			}
			return urlInfoFetchedMsg{err: fmt.Errorf("URL情報の取得に失敗:\n%s", string(output))}
		}
		var info ytDlpVideoInfo
		if err := json.Unmarshal(output, &info); err != nil {
			return urlInfoFetchedMsg{err: fmt.Errorf("URL情報のJSON解析に失敗:\n%v", err)}
		}
		artist := info.Uploader
		if artist == "" {
			artist = info.Channel
		}
		item := item{title: info.Title, desc: artist, id: info.ID, url: query}
		return urlInfoFetchedMsg{ytItem: item}
	}
}
func doMusicBrainzSearch(query string) ([]list.Item, error) {
	apiURL := fmt.Sprintf("https://musicbrainz.org/ws/2/release/?query=%s&fmt=json&inc=artist-credits+release-groups", url.QueryEscape(query))
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("User-Agent", "GoMusicDownloader/1.7 ( your-contact-info@example.com )")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var data MusicBrainzSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	var items []list.Item
	for _, r := range data.Releases {
		artist := joinArtistCredits(r.ArtistCredit)
		desc := fmt.Sprintf("%s (%s) [%s]", artist, r.Date, r.ReleaseGroup.PrimaryType)
		items = append(items, item{title: r.Title, desc: desc, id: r.ID, meta: r})
	}
	return items, nil
}
func searchMusicBrainzCmd(query string) tea.Cmd {
	return func() tea.Msg {
		items, err := doMusicBrainzSearch(query)
		if err != nil {
			return mbSearchFinishedMsg{err: err}
		}
		return mbSearchFinishedMsg{items: items}
	}
}
func searchCmd(ytDlpPath, query string) tea.Cmd {
	return func() tea.Msg {
		var wg sync.WaitGroup
		wg.Add(2)
		var ytItems, mbItems []list.Item
		var ytErr, mbErr error
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, ytDlpPath, "--quiet", "--no-warnings", "--dump-json", "--default-search", "ytsearch5", query)
			output, err := cmd.CombinedOutput()
			if err != nil {
				if ctx.Err() == context.DeadlineExceeded {
					ytErr = fmt.Errorf("YouTube検索がタイムアウトしました")
				} else {
					ytErr = fmt.Errorf("YouTube検索に失敗:\n%s", string(output))
				}
				return
			}
			var items []list.Item
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			for _, line := range lines {
				var info ytDlpVideoInfo
				if err := json.Unmarshal([]byte(line), &info); err != nil {
					continue
				}
				artist := info.Uploader
				if artist == "" {
					artist = info.Channel
				}
				items = append(items, item{title: info.Title, desc: artist, id: info.ID, url: "https://www.youtube.com/watch?v=" + info.ID})
			}
			ytItems = items
		}()
		go func() {
			defer wg.Done()
			mbItems, mbErr = doMusicBrainzSearch(query)
		}()
		wg.Wait()
		if ytErr != nil {
			return searchFinishedMsg{err: ytErr}
		}
		if mbErr != nil {
			return searchFinishedMsg{err: mbErr}
		}
		return searchFinishedMsg{ytItems: ytItems, mbItems: mbItems}
	}
}
func getTracklistCmd(releaseID string) tea.Cmd {
	return func() tea.Msg {
		apiURL := fmt.Sprintf("https://musicbrainz.org/ws/2/release/%s?inc=artist-credits+media+recordings&fmt=json", releaseID)
		req, _ := http.NewRequest("GET", apiURL, nil)
		req.Header.Set("User-Agent", "GoMusicDownloader/1.7 ( your-contact-info@example.com )")
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return tracklistFinishedMsg{err: err}
		}
		defer resp.Body.Close()
		var releaseData MBRelease
		if err := json.NewDecoder(resp.Body).Decode(&releaseData); err != nil {
			return tracklistFinishedMsg{err: err}
		}
		var items []list.Item
		artist := joinArtistCredits(releaseData.ArtistCredit)
		for _, media := range releaseData.Media {
			for _, t := range media.Tracks {
				desc := fmt.Sprintf("Track %s", t.Number)
				if media.Format != "" {
					desc = fmt.Sprintf("Track %s (%s)", t.Number, media.Format)
				}
				items = append(items, item{title: t.Title, desc: desc, meta: t, artist: artist})
			}
		}
		return tracklistFinishedMsg{items: items}
	}
}
func getLyrics(artist, title, album string, duration int) string {
	apiURL := "https://lrclib.net/api/get"
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		log.Printf("Lyrics: Failed to create request: %v", err)
		return ""
	}
	q := req.URL.Query()
	q.Add("track_name", title)
	q.Add("artist_name", artist)
	q.Add("album_name", album)
	q.Add("duration", fmt.Sprintf("%d", duration))
	req.URL.RawQuery = q.Encode()

	log.Printf("Lyrics: Calling API: %s", req.URL.String())

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Lyrics: API request failed: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Lyrics: API returned non-200 status: %s", resp.Status)
		return ""
	}

	var data LrclibResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		log.Printf("Lyrics: Failed to decode JSON response: %v", err)
		return ""
	}
	return data.PlainLyrics
}
func simpleDownloadCmd(ytDlpPath, ffmpegPath string, selectedYT item) tea.Cmd {
	return func() tea.Msg {
		tmpDirPath := filepath.Join(mainDir, tempDir)
		tmpDir, err := os.MkdirTemp(tmpDirPath, "gomusicdl_*")
		if err != nil {
			return downloadFinishedMsg{err: err}
		}
		defer os.RemoveAll(tmpDir)
		audioPath := filepath.Join(tmpDir, "audio.tmp")
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout*2) // ダウンロードは長めに
		defer cancel()
		dlCmd := exec.CommandContext(ctx, ytDlpPath, "-f", "bestaudio", "-o", audioPath, selectedYT.url)
		if out, err := dlCmd.CombinedOutput(); err != nil {
			return downloadFinishedMsg{err: fmt.Errorf("音声のダウンロード失敗:\n%s", string(out))}
		}
		downloadsPath := filepath.Join(mainDir, downloadsDir)
		finalFilename := sanitizeFilename(fmt.Sprintf("%s.flac", selectedYT.title))
		finalPath := filepath.Join(downloadsPath, finalFilename)
		convCmd := exec.Command(ffmpegPath, "-y", "-i", audioPath, "-c:a", "flac", finalPath)
		if out, err := convCmd.CombinedOutput(); err != nil {
			return downloadFinishedMsg{err: fmt.Errorf("ffmpegでの変換失敗:\n%s", string(out))}
		}
		return downloadFinishedMsg{filename: finalPath}
	}
}
func downloadCmd(ytDlpPath, ffmpegPath string, selectedYT, selectedMB item, tags finalTags) tea.Cmd {
	return func() tea.Msg {
		var wg sync.WaitGroup
		wg.Add(3)
		var audioPath, coverPath, lyrics string
		var dlErr error

		tmpDirPath := filepath.Join(mainDir, tempDir)
		tmpDir, err := os.MkdirTemp(tmpDirPath, "gomusicdl_*")
		if err != nil {
			return downloadFinishedMsg{err: err}
		}
		defer os.RemoveAll(tmpDir)

		go func() {
			defer wg.Done()
			audioPath = filepath.Join(tmpDir, "audio.tmp")
			ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout*2)
			defer cancel()
			dlCmd := exec.CommandContext(ctx, ytDlpPath, "-f", "bestaudio", "-o", audioPath, selectedYT.url)
			if out, err := dlCmd.CombinedOutput(); err != nil {
				dlErr = fmt.Errorf("音声のダウンロード失敗:\n%s", string(out))
			}
		}()

		go func() {
			defer wg.Done()
			releaseInfo := selectedMB.meta.(MBRelease)
			coverURL := fmt.Sprintf("https://coverartarchive.org/release/%s/front-500", releaseInfo.ID)
			resp, err := http.Get(coverURL)
			if err == nil && resp.StatusCode == 200 {
				localPath := filepath.Join(tmpDir, "cover.jpg")
				file, _ := os.Create(localPath)
				io.Copy(file, resp.Body)
				file.Close()
				coverPath = localPath
			}
			if resp != nil {
				resp.Body.Close()
			}

			if coverPath == "" && releaseInfo.ReleaseGroup.ID != "" {
				coverGroupURL := fmt.Sprintf("https://coverartarchive.org/release-group/%s/front-500", releaseInfo.ReleaseGroup.ID)
				resp, err = http.Get(coverGroupURL)
				if err == nil && resp.StatusCode == 200 {
					localPath := filepath.Join(tmpDir, "cover.jpg")
					file, _ := os.Create(localPath)
					io.Copy(file, resp.Body)
					file.Close()
					coverPath = localPath
				}
				if resp != nil {
					resp.Body.Close()
				}
			}
		}()

		go func() {
			defer wg.Done()
			lyrics = getLyrics(tags.Artist, tags.Title, tags.Album, tags.DurationSec)
		}()

		wg.Wait()

		if dlErr != nil {
			return downloadFinishedMsg{err: dlErr}
		}

		downloadsPath := filepath.Join(mainDir, downloadsDir)
		finalFilename := sanitizeFilename(fmt.Sprintf("%s - %s.flac", tags.Artist, tags.Title))
		finalPath := filepath.Join(downloadsPath, finalFilename)

		ffmpegArgs := []string{"-y", "-i", audioPath}
		if coverPath != "" {
			ffmpegArgs = append(ffmpegArgs, "-i", coverPath, "-map", "0:a:0", "-map", "1:v:0", "-disposition:v", "attached_pic")
		}
		ffmpegArgs = append(ffmpegArgs,
			"-c:a", "flac",
			"-metadata", fmt.Sprintf("title=%s", tags.Title),
			"-metadata", fmt.Sprintf("artist=%s", tags.Artist),
			"-metadata", fmt.Sprintf("album_artist=%s", tags.AlbumArtist),
			"-metadata", fmt.Sprintf("album=%s", tags.Album),
			"-metadata", fmt.Sprintf("track=%s", tags.TrackNumber),
			"-metadata", fmt.Sprintf("date=%s", tags.Date),
		)
		if lyrics != "" {
			ffmpegArgs = append(ffmpegArgs, "-metadata", fmt.Sprintf("LYRICS=%s", lyrics))
		}
		ffmpegArgs = append(ffmpegArgs, finalPath)

		convCmd := exec.Command(ffmpegPath, ffmpegArgs...)
		if out, err := convCmd.CombinedOutput(); err != nil {
			return downloadFinishedMsg{err: fmt.Errorf("ffmpegでの変換失敗:\n%s", string(out))}
		}

		finalMsg := finalPath
		if lyrics != "" {
			finalMsg += " (歌詞付き)"
		}
		return downloadFinishedMsg{filename: finalMsg}
	}
}
func sanitizeFilename(name string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "'", "<", "-", ">", "-", "|", "-")
	return r.Replace(name)
}
func setupAppDirs() error {
	dirs := []string{mainDir, filepath.Join(mainDir, downloadsDir), filepath.Join(mainDir, tempDir), filepath.Join(mainDir, logsDir)}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return err
		}
	}
	return nil
}
func main() {
	if err := setupAppDirs(); err != nil {
		fmt.Printf("ディレクトリの作成に失敗しました: %v\n", err)
		os.Exit(1)
	}
	logPath := filepath.Join(mainDir, logsDir, "debug.log")
	f, err := tea.LogToFile(logPath, "debug")
	if err != nil {
		fmt.Printf("ログファイルの作成に失敗しました: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	p := tea.NewProgram(newModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("アプリケーションエラー: %v", err)
		os.Exit(1)
	}
}
