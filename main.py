import argparse
import json
import logging
import random
import time


class JsonFormatter(logging.Formatter):
    def format(self, record):
        log_record = {
            "timestamp": self.formatTime(record, self.datefmt),
            "level": record.levelname,
            "message": record.getMessage(),
        }
        return json.dumps(log_record)


def setup_logging():
    """Sets up the logging configuration."""
    logger = logging.getLogger()
    if logger.hasHandlers():
        logger.handlers.clear()
    logger.setLevel(logging.INFO)
    handler = logging.StreamHandler()
    # Use the custom JSON formatter
    formatter = JsonFormatter(datefmt="%Y-%m-%d %H:%M:%S")
    handler.setFormatter(formatter)
    logger.addHandler(handler)


def generate_logs(rate, duration):
    """Generates logs at a specified rate for a given duration."""
    end_time = time.time() + duration if duration else None
    log_levels = [logging.INFO, logging.WARNING, logging.ERROR]
    log_messages = [
        "User logged in successfully",
        "Failed to connect to database",
        "Invalid input received",
        "Processing request",
        "Request completed",
    ]

    while not end_time or time.time() < end_time:
        level = random.choice(log_levels)
        message = random.choice(log_messages)
        logging.log(level, message)
        time.sleep(1 / rate)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Kubernetes Log Generator")
    parser.add_argument(
        "--rate", type=int, default=10, help="Logs per second"  # Increased rate
    )
    parser.add_argument(
        "--duration", type=int, help="Duration in seconds (optional)"
    )
    args = parser.parse_args()

    setup_logging()
    generate_logs(args.rate, args.duration)