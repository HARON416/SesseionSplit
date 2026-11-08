// Copyright 2026 The SessionSplit Authors
// Use of this source code is governed by a BSD-style license that can be
// found in the Chromium LICENSE file.

#include "components/embedder_support/sessionsplit_profile_config.h"

#include <utility>

#include "base/json/json_reader.h"
#include "base/json/json_writer.h"
#include "base/values.h"
#include "testing/gtest/include/gtest/gtest.h"

namespace embedder_support {
namespace {

base::DictValue ValidInput() {
  return base::JSONReader::ReadDict(
             R"({"schemaVersion":1,
                 "seed":"0101010101010101010101010101010101010101010101010101010101010101",
                 "languages":["EN-us","en"],"timezone":"UTC"})",
             base::JSON_PARSE_RFC)
      .value();
}

auto Parse(base::DictValue dict) {
  return ParseSessionSplitProfileConfig(base::WriteJson(dict).value());
}

TEST(SessionSplitProfileConfigTest, ParsesSeedAndCanonicalEnvironment) {
  auto result = Parse(ValidInput());
  ASSERT_TRUE(result.has_value());
  EXPECT_EQ(result->seed.front(), 1);
  EXPECT_EQ(result->seed.back(), 1);
  EXPECT_EQ(result->languages, (std::vector<std::string>{"en-US", "en"}));
  EXPECT_EQ(result->timezone, "Etc/UTC");
}

TEST(SessionSplitProfileConfigTest, RejectsUnknownAndWronglyTypedVersions) {
  auto dict = ValidInput();
  dict.Set("schemaVersion", 2);
  EXPECT_FALSE(Parse(std::move(dict)).has_value());
  dict = ValidInput();
  dict.Set("schemaVersion", "1");
  EXPECT_FALSE(Parse(std::move(dict)).has_value());
  dict = ValidInput();
  dict.Set("schemaVersion", true);
  EXPECT_FALSE(Parse(std::move(dict)).has_value());
}

TEST(SessionSplitProfileConfigTest, RejectsMissingAndUnknownFields) {
  auto dict = ValidInput();
  dict.Remove("timezone");
  EXPECT_FALSE(Parse(std::move(dict)).has_value());
  dict = ValidInput();
  dict.Set("password", "must-not-enter-native-profile-config");
  auto result = Parse(std::move(dict));
  ASSERT_FALSE(result.has_value());
  EXPECT_EQ(result.error().find("must-not-enter"), std::string::npos);
}

TEST(SessionSplitProfileConfigTest, RejectsInvalidSeedsWithoutEchoingInput) {
  for (const auto& seed : {std::string(), std::string(63, 'a'),
                           std::string(64, 'g'), std::string(64, 'A')}) {
    auto dict = ValidInput();
    dict.Set("seed", seed);
    EXPECT_FALSE(Parse(std::move(dict)).has_value());
  }
}

TEST(SessionSplitProfileConfigTest, RejectsInvalidAndDuplicateLanguages) {
  for (const auto& json : {"[]", "[123]", "[\"not a language\"]",
                           "[\"en-US\",\"EN-us\"]"}) {
    auto dict = ValidInput();
    dict.Set("languages", base::JSONReader::ReadList(json, base::JSON_PARSE_RFC)
                              .value());
    EXPECT_FALSE(Parse(std::move(dict)).has_value());
  }
  auto dict = ValidInput();
  dict.Set("languages", "en-US");
  EXPECT_FALSE(Parse(std::move(dict)).has_value());
}

TEST(SessionSplitProfileConfigTest, RejectsUnknownAndCustomOffsetTimezones) {
  for (const auto* zone : {"Mars/Olympus", "GMT+03:00", "Africa/Nairobi\n"}) {
    auto dict = ValidInput();
    dict.Set("timezone", zone);
    EXPECT_FALSE(Parse(std::move(dict)).has_value());
  }
  auto dict = ValidInput();
  dict.Set("timezone", "Africa/Nairobi");
  auto result = Parse(std::move(dict));
  ASSERT_TRUE(result.has_value());
  EXPECT_EQ(result->timezone, "Africa/Nairobi");
}

TEST(SessionSplitProfileConfigTest, RejectsMalformedAndOversizedInput) {
  EXPECT_FALSE(ParseSessionSplitProfileConfig("[]").has_value());
  EXPECT_FALSE(ParseSessionSplitProfileConfig("{broken").has_value());
  EXPECT_FALSE(ParseSessionSplitProfileConfig(
                   std::string(SessionSplitProfileConfig::kMaxBytes + 1, ' '))
                   .has_value());
}

}  // namespace
}  // namespace embedder_support
