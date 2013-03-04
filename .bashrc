
#for terminal

if [ "$TERM" == "screen" ]; then
  export PS1='\[\033[36m\][\u@\h:r \033[33m\]\w\[\033[36m\]]\[\033[0m\]\[\033[31m\] :$WINDOW: \e[m \nヽ| ・∀・|ノ$ '
  # export PS1='\h:$WINDOW:\w\$ '
fi

