interface IOSNavigator extends Navigator {
  standalone?: boolean;
}

export function isSafariBrowser() {
  const userAgent = navigator.userAgent;
  const isAppleWebKit = /AppleWebKit/i.test(userAgent);
  const isSafari = /Safari/i.test(userAgent);
  const isAnotherBrowser = /(CriOS|FxiOS|EdgiOS|OPiOS|Chrome|Chromium|Android)/i.test(userAgent);
  const isAppleStandalone = (
    (navigator as IOSNavigator).standalone === true
    || window.matchMedia('(display-mode: standalone)').matches
  ) && /(iPhone|iPad|iPod)/i.test(userAgent);
  return isAppleWebKit && !isAnotherBrowser && (isSafari || isAppleStandalone);
}
