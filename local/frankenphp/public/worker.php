<?php

$maxRequests = 1000;
for ($nbRequests = 0; $nbRequests < $maxRequests; ++$nbRequests) {
    $keepRunning = \frankenphp_handle_request(function () {
        $ms = \random_int(10, 350);
        \usleep($ms * 1000);

        // Vary status codes by path so load.sh produces 200/404/500 like the caddy stack.
        $path = \parse_url($_SERVER['REQUEST_URI'] ?? '/', \PHP_URL_PATH);
        match ($path) {
            '/notfound' => \http_response_code(404),
            '/error' => \http_response_code(500),
            default => null,
        };

        \header('Content-Type: application/json');
        echo \json_encode([
            'ms' => $ms,
            'uniqid' => \uniqid(),
            'worker' => true,
        ]);
    });

    if (!$keepRunning) {
        break;
    }
}
