// The poll is the console's only moving part, so its failure has to be
// visible. htmx does not swap on a failed request: the ledger would keep
// showing the last rows it got, looking live, and send the reader off
// debugging the wrong thing.
(function () {
	function setOffline(on) {
		document.body.classList.toggle('is-offline', on);
	}

	document.body.addEventListener('htmx:sendError', function () {
		setOffline(true);
	});
	document.body.addEventListener('htmx:responseError', function () {
		setOffline(true);
	});
	document.body.addEventListener('htmx:afterRequest', function (event) {
		if (event.detail && event.detail.successful) {
			setOffline(false);
		}
	});
})();
