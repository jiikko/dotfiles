# 0) alias
alias al='alias'

alias va='vagrant'
alias bu='bundle install'

if [ `uname` = "Darwin" ]; then
    #mac用のコード
    alias ls='ls -G'
elif [ `uname` = "Linux" ]; then
    #Linux用のコード
    alias ls='ls -G'
fi

alias ll='ls -l'
alias ks='ls'

alias gst='git status'
alias gd='git diff'
alias gl='git log'
alias gb='git branch'
alias gch='git checkout'
alias g='git'
alias gv='git --version'
alias gag='git add .gitignore'
alias gcig='git commit -m "modied ignore"'

alias rdm='rake db:migrate'
alias r='rails'

alias rr='rake routes'

alias sb='source ~/.bash_profile'

alias r='rvm'
alias rc='rvm current'

alias sshge='ssh kjdev@s11.rs2.gehirn.jp'

alias v='vim'
alias vb='vim ~/.bashrc'
alias vbpro='vim ~/.bash_profile'
alias vv='vim ~/.vimrc'
alias ign='vim .gitignore'


# 1)
# bashのプロンプトにgitのブランチ名を表示する
# http://d.hatena.ne.jp/deeeki/20110402/git_branch_ps1
# http://d.hatena.ne.jp/jiikko/20130302#1362194668
if [ -f /opt/local/etc/profile.d/bash_completion.sh ]; then
  . /opt/local/etc/profile.d/bash_completion.sh
fi

# http://architects.dzone.com/articles/bash-gitps1-command-not-found
if [ -f /opt/local/share/doc/git-core/contrib/completion/git-prompt.sh ]; then
  . /opt/local/share/doc/git-core/contrib/completion/git-prompt.sh
fi

# 2) ターミナルの文字をかえる
#for terminal
#if [ "$TERM" == "screen" ]; then
#  export PS1='\[\033[36m\][\u@\h:r \033[33m\]\w\[\033[36m\]]\[\033[0m\]\[\033[31m\] :$WINDOW: \e[m \nヽ| ・∀・|ノ$ '
#fi

PS1='\[\033[36m\][\u@\h:\[\033[33m\]\w\[\033[36m\]]\[\033[0m\]\[\033[31m\]$(__git_ps1) \e[m \nヽ| ・∀・|ノ$ '
