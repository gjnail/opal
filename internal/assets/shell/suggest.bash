# opal: as-you-type suggestions for bash
# Readline can't draw grey as-you-type suggestions; ble.sh (a drop-in line
# editor for bash) can, along with syntax highlighting. When it's installed,
# opal loads it; otherwise bash keeps plain readline. Get it from:
#   https://github.com/akinomyoga/ble.sh
if [[ $- == *i* && -z ${BLE_VERSION-} ]] && (( BASH_VERSINFO[0] >= 4 )); then
  for __opal_f in "${XDG_DATA_HOME:-$HOME/.local/share}/blesh/ble.sh" /usr/share/blesh/ble.sh \
      /usr/local/share/blesh/ble.sh /opt/homebrew/share/blesh/ble.sh; do
    if [[ -f $__opal_f ]]; then
      source "$__opal_f" --attach=none && _opal_blesh=1
      break
    fi
  done
  unset __opal_f
fi
