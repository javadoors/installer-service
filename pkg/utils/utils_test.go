/*
 * Copyright (c) 2025 Huawei Technologies Co., Ltd.
 * openFuyao is licensed under Mulan PSL v2.
 * You can use this software according to the terms and conditions of the Mulan PSL v2.
 * You may obtain a copy of Mulan PSL v2 at:
 *          http://license.coscl.org.cn/MulanPSL2
 * THIS SOFTWARE IS PROVIDED ON AN "AS IS" BASIS, WITHOUT WARRANTIES OF ANY KIND,
 * EITHER EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO NON-INFRINGEMENT,
 * MERCHANTABILITY OR FIT FOR A PARTICULAR PURPOSE.
 * See the Mulan PSL v2 for more details.
 */

package utils

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"gopkg.openfuyao.cn/cluster-api-provider-bke/api/bkecommon/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsChanClosed(t *testing.T) {
	t.Run("检测已关闭channel", func(t *testing.T) {
		ch := make(chan int)
		close(ch)
		assert.True(t, IsChanClosed(ch))
	})

	t.Run("检测未关闭channel", func(t *testing.T) {
		ch := make(chan int)
		assert.False(t, IsChanClosed(ch))
		// 可选：清理未关闭的 channel（避免资源泄漏）
		close(ch)
	})

	t.Run("非channel类型panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("预期panic未发生")
			}
		}()
		IsChanClosed("not a channel")
	})
}

func TestConvertToChinaTime(t *testing.T) {
	// 测试正常转换
	t.Run("正常时间转换", func(t *testing.T) {
		testTime := metav1.NewTime(time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC))
		expected := "2023-05-15 18:30:00" // UTC+8

		result, err := ConvertToChinaTime(testTime)
		if err != nil {
			t.Fatalf("预期无错误，但得到: %v", err)
		}
		if result != expected {
			t.Errorf("预期 %s，得到 %s", expected, result)
		}
	})

	// 测试时区加载错误（通过mock time.LoadLocation）
	t.Run("时区加载错误", func(t *testing.T) {
		patches := gomonkey.NewPatches()
		defer patches.Reset()

		patches.ApplyFunc(time.LoadLocation, func(name string) (*time.Location, error) {
			return nil, fmt.Errorf("mock error")
		})

		_, err := ConvertToChinaTime(metav1.NewTime(time.Now()))
		if err == nil {
			t.Error("预期时区加载错误，但未返回错误")
		}
	})
}

func TestFindExtraNodes(t *testing.T) {
	tests := []struct {
		name      string
		bcNodes   []v1beta1.Node
		realNodes []v1.Node
		expected  []v1beta1.Node
	}{
		{
			name: "没有多余节点",
			bcNodes: []v1beta1.Node{
				{IP: "ab1"},
				{IP: "ab2"},
			},
			realNodes: []v1.Node{
				{Status: v1.NodeStatus{Addresses: []v1.NodeAddress{
					{Type: v1.NodeInternalIP, Address: "ab1"},
				}}},
				{Status: v1.NodeStatus{Addresses: []v1.NodeAddress{
					{Type: v1.NodeInternalIP, Address: "ab2"},
				}}},
			},
			expected: []v1beta1.Node{},
		},
		{
			name:      "空输入测试",
			bcNodes:   []v1beta1.Node{},
			realNodes: []v1.Node{},
			expected:  []v1beta1.Node{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FindExtraNodes(tt.bcNodes, tt.realNodes)
			if len(result) != len(tt.expected) {
				t.Errorf("预期 %d 个多余节点，得到 %d", len(tt.expected), len(result))
				return
			}
			for i := range result {
				if result[i].IP != tt.expected[i].IP {
					t.Errorf("预期多余节点 %s，得到 %s", tt.expected[i].IP, result[i].IP)
				}
			}
		})
	}
}
