const playerFrameStyleText = `
html,body,#game,#retrom-emulator,.ejs_parent,.ejs_game,.ejs_canvas_parent{width:100%!important;height:100%!important;margin:0!important;overflow:hidden;background:#05060a}
.ejs_canvas_parent{display:grid!important;place-items:center!important}
canvas{display:block;max-width:none!important;max-height:none!important;margin:auto!important;image-rendering:pixelated!important}
.ejs_virtualGamepad_open{display:none!important}
.ejs_virtualGamepad_left,.ejs_virtualGamepad_right{bottom:20px!important}
html.retrom-native-menu-locked:not(.retrom-native-settings-open) .ejs_menu_bar{visibility:hidden!important;opacity:0!important;pointer-events:none!important}
html.retrom-native-menu-locked.retrom-native-settings-open .ejs_menu_bar{border:0!important;background:transparent!important;box-shadow:none!important;pointer-events:none!important}
html.retrom-native-menu-locked.retrom-native-settings-open .ejs_menu_bar>*{visibility:hidden!important;pointer-events:none!important}
html.retrom-native-menu-locked.retrom-native-settings-open .ejs_menu_bar>:has(>.ejs_settings_parent){visibility:visible!important}
html.retrom-native-menu-locked.retrom-native-settings-open .ejs_menu_bar>:has(>.ejs_settings_parent)>.ejs_menu_button{visibility:hidden!important;pointer-events:none!important}
html.retrom-native-menu-locked.retrom-native-settings-open .ejs_menu_bar .ejs_settings_parent{visibility:visible!important;pointer-events:auto!important}
`;

export function installPlayerFrameStyle(frameDocument: Document) {
  const style = frameDocument.createElement("style");
  style.dataset.retromPlayerFrame = "true";
  style.textContent = playerFrameStyleText;
  frameDocument.head.append(style);
  return style;
}
