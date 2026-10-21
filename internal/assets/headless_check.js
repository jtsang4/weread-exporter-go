// Evaluates to the list of reasons why this browser looks automated or
// headless (an empty array means it looks like a regular, visible Chrome).
// Derived from the headless detection of drunkdream/weread-exporter (MIT).
(() => {
  const reasons = [];
  if (navigator.webdriver === true) {
    reasons.push("navigator.webdriver is true");
  }
  if (typeof window.chrome === "undefined") {
    reasons.push("window.chrome is missing");
  }
  if (navigator.plugins.length === 0) {
    reasons.push("the browser reports no plugins");
  }
  if (!navigator.languages || navigator.languages.length === 0) {
    reasons.push("navigator.languages is empty");
  }
  if (/HeadlessChrome/.test(navigator.userAgent)) {
    reasons.push("the user agent contains HeadlessChrome");
  }
  if (window.outerWidth === 0 && window.outerHeight === 0) {
    reasons.push("the window has no size");
  }
  return reasons;
})()
