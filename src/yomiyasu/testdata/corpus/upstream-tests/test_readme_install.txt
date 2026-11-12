README の Codex 向けの入れ方（.agents/skills に置く手順）のコマンドを、そのまま sh で動かして確かめる。

README.md の「6. Codex で使うリポジトリに置く」と README.en.md の同じ手順のコマンドが同じ文字列であることと、
初めて入れるとき、利用者が書き換えたフォルダを入れ直すとき（前のフォルダを消さずに .agents/skills の外へ移す）、
続けて入れ直すとき（何も変えない）、取り出しに失敗したとき（前のフォルダに触れない）の動きを見る。

GitHub には接続しない。git の insteadOf で、README の URL をこのチェックアウトの skills/yomiyasu から作った
一時リポジトリに向け、GIT_ALLOW_PROTOCOL で file だけを許す（insteadOf が効かなくても外へ出ない）。HOME と TMPDIR は
試験の一時フォルダの中に向け、git の設定は GIT_CONFIG_GLOBAL の一時ファイルだけを読むので、利用者の設定と共有の /tmp には
触れない。macOS の mktemp -d は TMPDIR を見ずに利用者ごとの一時フォルダ（/var/folders の下）に作ることがあるので、
README のコマンドの前に、引数が -d だけの mktemp を TMPDIR の下に作り、作った場所を記録する sh の関数を置く
（README のコマンドは変えない）。こうして、取り出し用の一時フォルダがコマンドの終わりに消えることも、macOS と Linux で
同じように見る。
Windows と、sh か git がない環境、GIT_CONFIG_GLOBAL を読まない git（2.32 より前）では飛ばす。


### 6. Codex で使うリポジトリに置く

marker の文を含む段落のあとで最初に出る ```bash のブロック。

path の下のファイルの (相対パス, 中身) の組。

README のコマンドは POSIX の sh 向け

sh か git 2.32 以上がない

利用者の git の設定と環境の GIT_ の変数を読まない環境。HOME と TMPDIR は一時フォルダの中。

README のコマンドが mktemp -d で作ったフォルダ（作った順）。

README のコマンドが共有の /tmp を使っています。動かさずに止めます
