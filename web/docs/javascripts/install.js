// Fills #install-version on the install page from /api/latest, the worker's
// same-origin proxy of the stable channel manifest the app's updater reads.
// Any failure leaves the span empty and the page hides it.
(function () {
  function fill() {
    var target = document.getElementById("install-version");
    if (!target) {
      return;
    }

    fetch("/api/latest", { headers: { accept: "application/json" } })
      .then(function (response) {
        return response.ok ? response.json() : null;
      })
      .then(function (manifest) {
        var version = manifest && manifest.version;
        if (typeof version === "string" && version !== "") {
          target.textContent = "Latest release: v" + version;
        }
      })
      .catch(function () {});
  }

  // With instant navigation the page body is swapped in without a reload, so
  // the theme's document$ observable is the only reliable "page ready" hook.
  if (window.document$ && typeof window.document$.subscribe === "function") {
    window.document$.subscribe(fill);
  } else {
    fill();
  }
})();
