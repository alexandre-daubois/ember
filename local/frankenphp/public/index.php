<?php

$ms = \random_int(10, 350);
\usleep($ms * 1000);
\header('Content-Type: application/json');
echo \json_encode([
    'ms' => $ms,
    'uniqid' => \uniqid(),
]);
