🎵 yt-Music Downloader v1.0高品質な音楽ライブラリを構築するために生まれた、インテリジェントなコマンドラインツール。YouTubeから最高の音質の音源をダウンロードし、MusicBrainzの公式データベースから取得した正確なメタデータと、lrclibの歌詞データ、そして高解像度のアルバムアートを自動でFLACファイルに埋め込みます。✨ 主な機能インタラクティブなTUI: 洗練されたUIで、直感的に操作できます。高精度なメタデータ: MusicBrainzと連携し、曲名、アーティスト名、アルバム名、リリース年、トラック番号を自動で取得・埋め込み。歌詞の自動埋め込み: lrclib.netと連携し、歌詞データをファイルに埋め込みます。高解像度ジャケット: Cover Art Archiveから、可能な限り高画質なアルバムアートを取得します。柔軟な検索: 曲名やアーティスト名での検索に加え、YouTubeのURLを直接貼り付けての実行にも対応。インタラクティブなタグ編集: ダウンロード直前に、取得したメタデータを確認・編集できます。スキップ機能: MusicBrainzの結果が意図しない場合でも、タグ付けをスキップして素早くダウンロード可能。連続ダウンロード: 一つの処理が完了すると自動で入力画面に戻り、ストレスなく次の作業に移れます。クリーンなファイル管理: downloads, temp, logs フォルダを自動生成し、ファイルを整理します。🚀 インストール1. 必須ツールのインストールこのアプリケーションは、内部で2つの強力な外部ツール yt-dlp と ffmpeg を利用しています。お使いのOSに合わせて、先にこれらのツールをインストールしてください。macOS (Homebrewを使用):brew install yt-dlp ffmpeg
Windows (ScoopまたはChocolateyを使用):# Scoop
scoop install yt-dlp ffmpeg

# Chocolatey
choco install yt-dlp ffmpeg
Debian / Ubuntu:sudo apt-get update && sudo apt-get install -y yt-dlp ffmpeg
2. Go Music Downloaderの入手お使いのOSに合った実行ファイルを、GitHubのReleasesページからダウンロードしてください。ダウンロード後、実行ファイルを好きな場所に配置し、ターミナルからそのファイルを実行します。使い方ターミナル（コマンドプロンプト）を開き、ダウンロードした実行ファイルがあるディレクトリで、以下のコマンドを実行します。./go-music-downloader
Windowsの場合は、以下のようになります。.\go-music-downloader.exe
アプリケーションが起動したら、あとは画面の指示に従って操作してください。🛠️ ソースからのビルド (開発者向け)ご自身でソースコードを修正・ビルドしたい場合は、以下の手順に従ってください。1. Go言語環境のセットアップ公式サイトを参考に、Go言語の環境を構築してください。2. 依存関係のインストールプロジェクトのルートディレクトリで以下のコマンドを実行します。go mod tidy
3. ビルド以下のコマンドで、現在お使いのOS向けの実行ファイルが生成されます。go build -o go-music-downloader .
クロスコンパイル (他のOS向けにビルド)特定のOS向けの実行ファイルを生成するには、以下のコマンドを使用します。Windows (64-bit):GOOS=windows GOARCH=amd64 go build -o go-music-downloader.exe .
macOS (Apple Silicon):GOOS=darwin GOARCH=arm64 go build -o go-music-downloader-macos-arm64 .
macOS (Intel):GOOS=darwin GOARCH=amd64 go build -o go-music-downloader-macos-intel .
Linux (64-bit):GOOS=linux GOARCH=amd64 go build -o go-music-downloader-linux .
⚠️ 免責事項このツールは、技術的な興味と個人的な学習のために開発されました。ダウンロードするコンテンツの著作権については、利用者が所在する国の法律を遵守し、各自の責任において利用してください。開発者は、このツールの利用によって生じたいかなる問題についても責任を負いません。