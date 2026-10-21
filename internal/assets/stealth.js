// Browser fingerprint patches, derived from drunkdream/weread-exporter (MIT).
//
// A Chrome driven over the DevTools protocol differs from a regular one in a
// few details that web pages (and anti-abuse scripts) can observe. This script
// runs before any page script and hides those differences. It only touches
// properties that are actually off, so a regular browser is left alone.
(() => {
  if (navigator.webdriver) {
    Object.defineProperty(navigator, "webdriver", { get: () => undefined });

    const hasOwn = Object.prototype.hasOwnProperty;
    Object.prototype.hasOwnProperty = function (key) {
      if (key === "webdriver") {
        return false;
      }
      return hasOwn.call(this, key);
    };

    if (navigator.permissions && navigator.permissions.query) {
      const query = navigator.permissions.query.bind(navigator.permissions);
      navigator.permissions.query = (parameters) =>
        parameters && parameters.name === "notifications"
          ? Promise.resolve({ state: Notification.permission })
          : query(parameters);
    }
  }

  if (navigator.plugins.length === 0) {
    Object.defineProperty(navigator, "plugins", { get: () => [1, 2, 3, 4, 5] });
    Object.defineProperty(window, "PluginArray", { get: () => Array });
  }

  if (!navigator.languages || navigator.languages.length === 0) {
    Object.defineProperty(navigator, "languages", { get: () => ["en-US", "en"] });
  }

  window.chrome = window.chrome || { runtime: {} };
})();
